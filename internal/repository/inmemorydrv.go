package repository

import "errors"

var (
	ErrDBRecordNotFound = errors.New("record not found")
)

// Объявляем список используемых параметров конфига
type InMemoryDrvConfig interface {
}

type InMemoryDrv struct {
	cfg  InMemoryDrvConfig
	data []string
}

func NewInMemoryDrv(cfg InMemoryDrvConfig) *InMemoryDrv {
	return &InMemoryDrv{
		cfg: cfg,
	}
}

// Записывает какую-то строку в БД и возвращает строку-идентификатор записи
func (s *InMemoryDrv) UpSert(str string) (byte, uint64, error) {
	for idx, val := range s.data {
		if val == str {
			return 0, uint64(idx), nil
		}
	}
	idx := len(s.data)
	s.data = append(s.data, str)
	return 0, uint64(idx), nil
}

// По строке-идентификатору возвращает ранее записанную строку
func (s *InMemoryDrv) Select(shardID byte, idx uint64) (string, error) {
	if idx >= uint64(len(s.data)) {
		return "", ErrDBRecordNotFound
	}

	return s.data[idx], nil
}
