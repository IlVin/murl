package repository

import (
	"errors"
	"murl/internal/config"

	"github.com/cespare/xxhash/v2"
	"go.uber.org/zap"
)

var (
	ErrURLNotFound        error = errors.New("URL not found")
	ErrURLMappingNotSaved error = errors.New("the mapping between short URL and long URL is not saved")
)

// Объявляем список используемых параметров конфига
type IRepoConfig interface {
	config.IZapLogger
	IRepoDrvConfig
}

// Поддерживаем драйвера, которые работают с шардами
// Т.е. идентификатор 2х мерный: shardID + record_id
type IRepoDataDrv interface {
	IRepoDrv
}

type Repo struct {
	zap       *zap.Logger
	shardSize byte
	db        IRepoDataDrv
}

// Конструктор хранилища с драйвером
func NewRepo(cfg IRepoConfig) *Repo {
	// Адаптер к определенной БД. Оперируем: вычислить шард, записать строку, прочитать до цифровому ID
	drv := NewRepoDrv(cfg)

	return &Repo{
		zap:       cfg.Zap(),
		shardSize: cfg.ShardSize(),
		db:        drv,
	}
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
func (s *Repo) Save(longStr string) (byte, uint64, error) {
	// Запись в БД маппинга
	sID := GetShardID(longStr, s.shardSize)
	idx, err := s.db.UpSert(sID, longStr)
	if err != nil {
		return 0, 0, errors.Join(ErrURLMappingNotSaved, err)
	}

	return sID, idx, nil
}

// Чтение из БД потоко БЕЗОПАСНОЕ
func (s *Repo) Load(sID byte, idx uint64) (string, error) {
	lURL, err := s.db.Select(sID, idx)
	if err != nil {
		return "", errors.Join(ErrURLNotFound, err)
	}

	return lURL, nil
}
