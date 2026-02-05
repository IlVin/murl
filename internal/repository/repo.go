package repository

import (
	"bufio"
	"errors"
	"fmt"
	"murl/internal/config"
	"murl/internal/model"
	"os"

	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"
)

var (
	ErrURLNotFound        error = errors.New("URL not found")
	ErrURLMappingNotSaved error = errors.New("the mapping between short URL and long URL is not saved")
	ErrAddURLEventNotSent error = errors.New("cannot send event AddURL")
)

// Объявляем список используемых параметров конфига
type RepoConfig interface {
	config.ZapLogger
	RepoDrvConfig
	EventStoragePath() string
}

// Поддерживаем драйвера, которые работают с шардами
// Т.е. идентификатор 2х мерный: shardID + record_id
type RepoDataDrv interface {
	RepoDrv
}

type Repo struct {
	zap       *zap.Logger
	shardSize byte
	db        RepoDataDrv
	events    chan model.Event
}

// Конструктор хранилища с драйвером
func NewRepo(cfg RepoConfig) *Repo {
	// Адаптер к определенной БД. Оперируем: вычислить шард, записать строку, прочитать до цифровому ID
	drv := NewRepoDrv(cfg)

	r := &Repo{
		zap:       cfg.Zap(),
		shardSize: cfg.ShardSize(),
		db:        drv,
		events:    make(chan model.Event, 100),
	}

	r.LoadStoredEvents(cfg)

	go eventSaver(cfg, r.events)

	return r
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
func (r *Repo) Save(longStr string) (byte, uint64, error) {
	// Запись в БД маппинга
	sID := GetShardID(longStr, r.shardSize)
	idx, err := r.db.UpSert(sID, longStr)
	if err != nil {
		return 0, 0, errors.Join(ErrURLMappingNotSaved, err)
	}

	err = r.SendAddURLEvent(sID, idx, longStr)
	if err != nil {
		return 0, 0, errors.Join(ErrURLMappingNotSaved, err)
	}

	return sID, idx, nil
}

func (r *Repo) Set(sID byte, id uint64, u string) error {
	return r.db.Set(sID, id, u)
}

// Чтение из БД потоко БЕЗОПАСНОЕ
func (r *Repo) Load(sID byte, idx uint64) (string, error) {
	lURL, err := r.db.Select(sID, idx)
	if err != nil {
		return "", errors.Join(ErrURLNotFound, err)
	}

	return lURL, nil
}

func (r *Repo) Close() {
	close(r.events)
}

func (r *Repo) LoadStoredEvents(cfg RepoConfig) {
	// Если файл не задан, то выходим
	if cfg.EventStoragePath() == "" {
		return
	}

	// Чтение событий из файла
	fh, err := os.OpenFile(cfg.EventStoragePath(), os.O_RDONLY, 0666)
	if err != nil {
		cfg.Zap().Error("cannot open file",
			zap.Error(err),
		)
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)

	for scanner.Scan() {
		event, err := model.Parse(scanner.Bytes())
		if err != nil {
			cfg.Zap().Error("cannot parse event",
				zap.Error(err),
			)
		}
		p := &model.PayloadAddURL{}
		if err != nil {
			cfg.Zap().Error("cannot parse event",
				zap.Error(err),
			)
		}
		err = event.GetPayload(p)
		if err != nil {
			cfg.Zap().Error("cannot parse event",
				zap.Error(err),
			)
		}
		err = r.Set(p.ShardID, p.ID, p.URL)
		if err != nil {
			cfg.Zap().Error("cannot set event",
				zap.Error(err),
			)
		}
	}
}

func (r *Repo) SendAddURLEvent(sID byte, idx uint64, u string) error {
	event, payload, err := model.NewEvent[model.PayloadAddURL]()
	if err != nil {
		return errors.Join(ErrAddURLEventNotSent, err)
	}

	payload.URL = u
	payload.ShardID = sID
	payload.ID = idx

	err = event.SetPayload(payload)
	if err != nil {
		r.zap.Warn("cannot create event",
			zap.String("long_url", u),
			zap.Error(err),
		)
		return errors.Join(ErrAddURLEventNotSent, err)
	}

	// Отправка события на запись
	r.events <- event

	return nil
}

func eventSaver(cfg RepoConfig, ch <-chan model.Event) {
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
			cfg.Zap().Error("cannot serialize event",
				zap.Error(err),
			)
			continue
		}

		nn, err := w.Write(data)
		if err != nil || nn != len(data) {
			cfg.Zap().Error("save event problem",
				zap.Error(err),
			)
			return
		}
		err = w.WriteByte(0x0A)
		if err != nil {
			cfg.Zap().Error("save event problem",
				zap.Error(err),
			)
			return
		}
		err = w.Flush()
		if err != nil {
			cfg.Zap().Error("save event problem",
				zap.Error(err),
			)
			return
		}

	}
}
