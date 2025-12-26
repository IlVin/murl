package repository

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
func (s *InMemoryDrv) UpSert(str string) (byte, int, bool) {
	for idx, val := range s.data {
		if val == str {
			return 0, idx, true
		}
	}
	idx := len(s.data)
	s.data = append(s.data, str)
	return 0, idx, true
}

// По строке-идентификатору возвращает ранее записанную строку
func (s *InMemoryDrv) Select(shard_id byte, id int) (string, bool) {
	if id < 0 || id >= len(s.data) {
		return "", false
	}

	return s.data[id], true
}
