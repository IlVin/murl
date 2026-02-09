package repository

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"murl/internal/model/event"
	"murl/internal/repository/pgc"
	"os"

	"github.com/cespare/xxhash/v2"
)

//go:generate mockgen -source=$GOFILE -destination=repo_mocks_test.go -package=$GOPACKAGE

// Объявляем список используемых параметров конфига
type RepoConfig interface {
	RepoDrv() string
	ShardSize() byte
	DBDSN() string
	EventStoragePath() string
}

// Поддерживаем драйвера, которые работают с шардами
// Т.е. идентификатор 2х мерный: shardID + record_id
type RepoDataDrv interface {
	UpSert(ctx context.Context, shardID byte, str string) (uint64, error)
	Select(ctx context.Context, shardID byte, idx uint64) (string, error)
	Set(ctx context.Context, shardID byte, idx uint64, u string) error
}

type Repo struct {
	shardSize byte
	dbHndl    *pgc.PgHndl
	db        RepoDataDrv
	events    chan event.Event
}

// Конструктор хранилища с драйвером
func NewRepo(ctx context.Context, cfg RepoConfig) *Repo {
	// Адаптер к определенной БД. Оперируем: вычислить шард, записать строку, прочитать до цифровому ID
	drv, err := NewRepoDrv(ctx, cfg)

	if err == nil {
		loadStoredEvents(ctx, cfg, drv)
	}

	r := &Repo{
		shardSize: cfg.ShardSize(),
		db:        drv,
		events:    make(chan event.Event, 100),
	}

	go eventSaver(cfg, r.events)

	if cfg.DBDSN() != "" {
		h, err := pgc.NewPgHndl(context.Background(), "PgDB", cfg.DBDSN())
		if err != nil {
			slog.Error("cannot connect to PgDB",
				slog.Any("err", err),
				slog.String("DBDSN", cfg.DBDSN()),
			)
		} else {
			r.dbHndl = h
		}
	}

	return r
}

func loadStoredEvents(ctx context.Context, cfg RepoConfig, drv RepoDrv) {
	// Если файл не задан, то выходим
	if cfg.EventStoragePath() == "" {
		return
	}

	// Проверяем, существует ли файл вообще
	if _, err := os.Stat(cfg.EventStoragePath()); os.IsNotExist(err) {
		slog.Error("event storage not found",
			slog.String("path", cfg.EventStoragePath()),
		)
		return
	}

	// Чтение событий из файла
	fh, err := os.OpenFile(cfg.EventStoragePath(), os.O_RDONLY, 0666)
	if err != nil {
		slog.Error("cannot open file",
			slog.Any("err", err),
		)
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)

	for scanner.Scan() {
		evt, err := event.Parse(scanner.Bytes())
		if err != nil {
			slog.Error("cannot parse event",
				slog.Any("err", err),
			)
			continue
		}
		p := event.PayloadAddURL{}

		if evt.GetType() != event.EvAddURL {
			slog.Error("invalid event type",
				slog.Any("EventType", evt.GetType()),
			)
			continue
		}

		if err = evt.GetPayload(&p); err != nil {
			slog.Error("cannot get event payload",
				slog.Any("err", err),
			)
			continue
		}

		err = drv.Set(ctx, p.ShardID, p.ID, p.URL)
		if err != nil {
			slog.Error("failed to save event payload to DB",
				slog.Any("err", err),
				slog.Any("payload", p),
			)
		}
	}
}

func (r *Repo) Ping(ctx context.Context) error {
	if r.dbHndl == nil {
		return fmt.Errorf("connect string to database was not set")
	}
	return r.dbHndl.Ping(ctx)
}

// GetShardID возвращает ID шарда для строки.
// Используем uint64 для минимизации коллизий перед делением.
func GetShardID(key string, shardSize byte) byte {
	if shardSize == 0 {
		return 0
	}
	return byte(xxhash.Sum64String(key) % uint64(shardSize))
}

// Запись в БД потоко БЕЗОПАСНАЯ
// Записывает строку в БД
// Возвращает строковый идентификатор записи
func (r *Repo) Save(ctx context.Context, longStr string) (byte, uint64, error) {
	// Запись в БД маппинга
	sID := GetShardID(longStr, r.shardSize)
	idx, err := r.db.UpSert(ctx, sID, longStr)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to save URL mapping to the database: %w", err)
	}

	err = r.SendAddURLEvent(sID, idx, longStr)
	if err != nil {
		return 0, 0, fmt.Errorf("URL mapping event was not sent: %w", err)
	}

	return sID, idx, nil
}

func (r *Repo) Set(ctx context.Context, sID byte, id uint64, u string) error {
	return r.db.Set(ctx, sID, id, u)
}

// Чтение из БД потоко БЕЗОПАСНОЕ
func (r *Repo) Load(ctx context.Context, sID byte, idx uint64) (string, error) {
	lURL, err := r.db.Select(ctx, sID, idx)
	if err != nil {
		return "", fmt.Errorf("URL not found: %w", err)
	}

	return lURL, nil
}

func (r *Repo) Close() {
	close(r.events)
}

func (r *Repo) SendAddURLEvent(sID byte, idx uint64, u string) error {
	payload := event.PayloadAddURL{
		ShardID: sID,
		ID:      idx,
		URL:     u,
	}

	event, err := event.MakeEvent(payload)
	if err != nil {
		return fmt.Errorf("event construction failed: %w", err)
	}

	// Отправка события на запись
	r.events <- event

	return nil
}

func eventSaver(cfg RepoConfig, ch <-chan event.Event) {
	// Если писать в файл не надо, то просто вычитываем канал
	if cfg.EventStoragePath() == "" {
		for range ch {
		}
		return
	}

	// Открываем файл
	fh, err := os.OpenFile(cfg.EventStoragePath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(fmt.Sprintf("cannot open event storage file: %v", err))
	}
	defer fh.Close()
	w := bufio.NewWriter(fh)

	// Вычитываем канал и пишем
	for e := range ch {
		data, err := e.Serialize()
		if err != nil {
			slog.Error("cannot serialize event",
				slog.Any("err", err),
			)
			continue
		}

		nn, err := w.Write(data)
		if err != nil || nn != len(data) {
			slog.Error("save event problem",
				slog.Any("err", err),
			)
			return
		}
		err = w.WriteByte(0x0A)
		if err != nil {
			slog.Error("save event problem",
				slog.Any("err", err),
			)
			return
		}
		err = w.Flush()
		if err != nil {
			slog.Error("save event problem",
				slog.Any("err", err),
			)
			return
		}

	}
}
