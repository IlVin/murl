package repository

import (
	"errors"
	"murl/internal/config"
	"sync"

	"go.uber.org/zap"
)

var (
	ErrOutOfRange       = errors.New("shardID out of range")
	ErrDBRecordNotFound = errors.New("record not found")
)

type IRepoDrvConfig interface {
	config.IZapLogger
	RepoDrv() string
	ShardSize() byte
}

type IRepoDrv interface {
	UpSert(shardID byte, str string) (uint64, error)
	Select(shardID byte, idx uint64) (string, error)
	Set(shardID byte, idx uint64, u string) error
}

func NewRepoDrv(cfg IRepoDrvConfig) IRepoDrv {
	switch cfg.RepoDrv() {
	case "InMemory":
		return newInMemoryRepoDrv(cfg)
	case "PgDB":
		return newPgRepoDrv(cfg)
	}
	cfg.Zap().Error("Unknow repo driver",
		zap.String("RepoDrv", cfg.RepoDrv()),
	)
	panic("Unknow repo driver")
}

// =============================================
//
//	InMemory
//
// =============================================
type inMemoryShard struct {
	mu    sync.RWMutex
	data  []string
	index map[string]uint64 // Для O(1) поиска при UpSert
	_     [8]byte           // Padding для защиты от False Sharing
}

type InMemoryRepoDrv struct {
	zap    *zap.Logger
	shards []inMemoryShard
}

func newInMemoryRepoDrv(cfg IRepoDrvConfig) IRepoDrv {
	shardSize := int(cfg.ShardSize())

	s := &InMemoryRepoDrv{
		zap:    cfg.Zap(),
		shards: make([]inMemoryShard, shardSize),
	}

	for i := 0; i < shardSize; i++ {
		s.shards[i] = inMemoryShard{
			mu:    sync.RWMutex{},
			data:  make([]string, 0, 100),
			index: make(map[string]uint64, 100),
		}
	}

	s.zap.Info("Use InMemoryDrv")
	return s
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
func (s *InMemoryRepoDrv) UpSert(shardID byte, str string) (uint64, error) {
	if int(shardID) >= len(s.shards) {
		s.zap.Error("shard access out of bounds",
			zap.Uint8("received", shardID),
			zap.Int("limit", len(s.shards)),
		)
		return 0, ErrOutOfRange
	}

	shard := &s.shards[shardID]

	shard.mu.RLock()
	if idx, ok := shard.index[str]; ok {
		shard.mu.RUnlock()
		return idx, nil
	}
	shard.mu.RUnlock()

	shard.mu.Lock()
	if idx, ok := shard.index[str]; ok {
		shard.mu.Unlock()
		return idx, nil
	}
	idx := uint64(len(shard.data))
	shard.data = append(shard.data, str)
	shard.index[str] = idx
	shard.mu.Unlock()

	return idx, nil
}

func (s *InMemoryRepoDrv) Set(shardID byte, idx uint64, u string) error {
	if int(shardID) >= len(s.shards) {
		s.zap.Error("shard access out of bounds",
			zap.Uint8("received", shardID),
			zap.Int("limit", len(s.shards)),
		)
		return ErrOutOfRange
	}

	shard := &s.shards[shardID]

	shard.mu.Lock()
	if idx == uint64(len(shard.data)) {
		shard.data = append(shard.data, u)
	} else if idx > uint64(len(shard.data)) {
		newTail := make([]string, idx-uint64(len(shard.data)+1))
		shard.data = append(shard.data, newTail...)
		shard.data[idx] = u
	} else {
		shard.data[idx] = u
	}
	shard.index[u] = idx
	shard.mu.Unlock()

	return nil
}

// По строке-идентификатору возвращает ранее записанную строку
func (s *InMemoryRepoDrv) Select(shardID byte, idx uint64) (string, error) {
	if int(shardID) >= len(s.shards) {
		return "", ErrOutOfRange
	}

	shard := &s.shards[shardID]
	shard.mu.RLock()

	if idx >= uint64(len(shard.data)) {
		return "", ErrDBRecordNotFound
	}

	val := shard.data[idx]

	shard.mu.RUnlock()
	return val, nil
}

// =============================================
//
//	PgDrv
//
// =============================================
// Заглушка. Драйвер будет разработан позже
func newPgRepoDrv(cfg IRepoDrvConfig) IRepoDrv {
	cfg.Zap().Info("Use PgDrv")
	return newInMemoryRepoDrv(cfg)
}
