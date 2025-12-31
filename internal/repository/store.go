package repository

import (
	"errors"
	"sync"
)

var (
	ErrURLNotFound        error = errors.New("URL not found")
	ErrURLMappingNotSaved error = errors.New("the mapping between short URL and long URL is not saved")
)

// Объявляем список используемых параметров конфига
type StoreConfig interface {
}

// Поддерживаем драйвера, которые работают с шардами
// Т.е. идентификатор 2х мерный: shardID + record_id
type DataDriver interface {
	UpSert(str string) (byte, uint64, error)
	Select(sID byte, idx uint64) (string, error)
}

type Store struct {
	mux *sync.Mutex
	cfg StoreConfig
	db  DataDriver
}

// Конструктор хранилища с драйвером
func NewStore(cfg StoreConfig, drv DataDriver) *Store {
	return &Store{
		mux: &sync.Mutex{},
		cfg: cfg,
		db:  drv,
	}
}

// Запись в БД потоко БЕЗОПАСНАЯ
// Записывает строку в БД
// Возвращает строковый идентификатор записи
func (s *Store) Save(longStr string) (byte, uint64, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	// Запись в БД маппинга
	sID, idx, err := s.db.UpSert(longStr)
	if err != nil {
		return 0, 0, errors.Join(ErrURLMappingNotSaved, err)
	}

	return sID, idx, nil
}

// Чтение из БД потоко БЕЗОПАСНОЕ
func (s *Store) Load(sID byte, idx uint64) (string, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	lURL, err := s.db.Select(sID, idx)
	if err != nil {
		return "", errors.Join(ErrURLNotFound, err)
	}

	return lURL, nil
}
