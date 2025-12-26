package repository

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"sync"
)

// Объявляем список используемых параметров конфига
type StoreConfig interface {
}

// Поддерживаем драйвера, которые работают с шардами
// Т.е. идентификатор 2х мерный: shard_id + record_id
type DataDriver interface {
	UpSert(str string) (byte, int, bool)
	Select(shard_id byte, id int) (string, bool)
}

type Store struct {
	mux      *sync.Mutex
	cfg      StoreConfig
	b64u     *base64.Encoding
	sid2char string
	db       DataDriver
}

// Конструктор хранилища с драйвером
func NewStore(cfg StoreConfig, drv DataDriver) *Store {
	return &Store{
		mux:      &sync.Mutex{},
		cfg:      cfg,
		b64u:     base64.URLEncoding.WithPadding(base64.NoPadding),
		sid2char: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_",
		db:       drv,
	}
}

// Запись в БД потоко БЕЗОПАСНАЯ
// Записывает строку в БД
// Возвращает строковый идентификатор записи
func (s *Store) Save(longStr string) (string, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	// Запись в БД маппинга
	shard_id, idx, ok := s.db.UpSert(longStr)
	if !ok {
		return "", ErrURLMappingNotSaved
	}
	sUrl := s.sid2char[shard_id:shard_id+1] + s.idx2str(idx)

	return sUrl, nil
}

// Чтение из БД потоко БЕЗОПАСНОЕ
func (s *Store) Load(id string) (string, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	pos := strings.Index(s.sid2char, id[:1])
	if pos < 0 {
		return "", ErrURLNotFound
	}
	shard_id := byte(pos)

	idx, ok := s.str2idx(id[1:])
	if !ok {
		return "", ErrURLNotFound
	}
	lUrl, ok := s.db.Select(shard_id, idx)
	if !ok {
		return lUrl, ErrURLNotFound
	}

	return lUrl, nil
}

func (s *Store) idx2str(idx int) string {
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutVarint(buf, int64(idx))

	return s.b64u.EncodeToString(buf[:n])
}

func (s *Store) str2idx(str string) (int, bool) {
	data, err := s.b64u.DecodeString(str)
	if err != nil {
		return 0, false
	}
	idx, n := binary.Varint(data)
	if n != len(data) {
		return 0, false
	}

	return int(idx), true
}
