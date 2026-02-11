package repository

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"murl/internal/model/event"
	"os"
	"sync"
	"time"

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
	BatchUpSert(ctx context.Context, batch []event.PayloadBatchItem) ([]event.PayloadBatchItem, error)
	Select(ctx context.Context, shardID byte, idx uint64) (string, error)
	Set(ctx context.Context, shardID byte, idx uint64, u string) error
	Ping(ctx context.Context) error
}

type Repo struct {
	shardSize byte
	db        RepoDataDrv
	events    chan event.Event
	wg        sync.WaitGroup
	walFile   *os.File
	wal       *bufio.Writer
}

// Конструктор хранилища с драйвером
func NewRepo(ctx context.Context, cfg RepoConfig) *Repo {
	// Адаптер к определенной БД. Оперируем: вычислить шард, записать строку, прочитать до цифровому ID
	drv, err := NewRepoDrv(ctx, cfg)

	if err == nil && drv != nil {
		loadStoredEvents(ctx, cfg, drv)
	}

	r := &Repo{
		shardSize: cfg.ShardSize(),
		db:        drv,
		events:    make(chan event.Event, 100),
		wg:        sync.WaitGroup{},
		wal:       nil,
	}

	if cfg.EventStoragePath() != "" {
		if fh, err := os.OpenFile(cfg.EventStoragePath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			r.walFile = fh
			r.wal = bufio.NewWriter(fh)
		} else {
			slog.Error("cannot open event storage file",
				slog.Any("err", err),
			)
		}
	}

	r.wg.Add(1)
	go r.eventSaver(cfg, r.events)
	time.Sleep(1 * time.Second)

	return r
}

func loadStoredEvents(ctx context.Context, cfg RepoConfig, drv RepoDrv) {
	// Если файл не задан, то выходим
	if cfg.EventStoragePath() == "" {
		return
	}

	// Проверяем, существует ли файл вообще
	if _, err := os.Stat(cfg.EventStoragePath()); os.IsNotExist(err) {
		slog.Info("event storage not found",
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
		return
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

		switch evt.GetType() {
		case event.EvBatch:
			p := event.PayloadBatch{}
			if err := evt.GetPayload(&p); err != nil {
				slog.Error("cannot get event payload",
					slog.Any("err", err),
				)
				continue
			}
			if _, err := drv.BatchUpSert(ctx, p); err != nil {
				slog.Error("failed to save event payload to DB",
					slog.Any("err", err),
					slog.Any("payload", p),
				)
				continue
			}
		case event.EvAddURL:
			p := event.PayloadAddURL{}
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
				continue
			}
		}
	}
}

func (r *Repo) Ping(ctx context.Context) error {
	return r.db.Ping(ctx)
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

	p := event.PayloadAddURL{
		ShardID: sID,
		ID:      idx,
		URL:     longStr,
	}

	e, err := event.MakeEvent(p, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("event construction failed: %w", err)
	}

	// Отправка события на запись
	r.PushEvent(ctx, e)

	return sID, idx, nil
}

func (r *Repo) PushEvent(ctx context.Context, e event.Event) {
	select {
	case r.events <- e:
		return
	case <-ctx.Done():
		return
	}
}

// Batch получает событие типа event.EvBatch и возвращает в качестве результата событие event.EvBatch
func (r *Repo) Batch(ctx context.Context, p event.PayloadBatch) (event.PayloadBatch, error) {
	// Вычисляем ShardID для каждого OrigURL
	for i := 0; i < len(p); i++ {
		p[i].ShardID = GetShardID(p[i].OrigURL, r.shardSize)
	}

	// Запись в БД маппинга
	// Если r.db вернул ошибку, то ее надо залогировать и отправить обратно батч с ошибками
	p, err := r.db.BatchUpSert(ctx, p)
	if err != nil {
		slog.Error("failed to save batch URL mapping to the database",
			slog.Any("err", err),
		)
	}

	return p, nil
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
	r.wg.Wait()
}

func (r *Repo) eventSaver(cfg RepoConfig, ch <-chan event.Event) {
	defer r.wg.Done()
	defer func() {
		if r.wal != nil {
			r.wal.Flush()
		}
		if r.walFile != nil {
			r.walFile.Close()
		}
	}()

	// Если файл не открыт, то просто вычитываем канал
	if r.wal == nil {
		for range ch {
		}
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.wal.Flush(); err != nil {
				slog.Error("cannot flush WAL",
					slog.Any("err", err),
				)
				continue
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, err := e.Serialize()
			if err != nil {
				slog.Error("cannot serialize event",
					slog.Any("err", err),
				)
				continue
			}

			if _, err := r.wal.Write(data); err != nil {
				slog.Error("save event problem",
					slog.Any("err", err),
				)
				continue
			}

			if err = r.wal.WriteByte(0x0A); err != nil {
				slog.Error("save event problem",
					slog.Any("err", err),
				)
				continue
			}

			// Iter9 требует немедленного сохранения на диск
			// Если нужна производительность, то здесь не Flush'им
			if err := r.wal.Flush(); err != nil {
				slog.Error("flush event problem",
					slog.Any("err", err),
				)
				continue
			}
		}
	}
}
