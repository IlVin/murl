package repository

import (
	"fmt"
	"log/slog"
	"sync"
)

type RepoDrvConfig interface {
	RepoDrv() string
	ShardSize() byte
	DBDSN() string
}

type RepoDrv interface {
	UpSert(shardID byte, str string) (uint64, error)
	Select(shardID byte, idx uint64) (string, error)
	Set(shardID byte, idx uint64, u string) error
}

func NewRepoDrv(cfg RepoDrvConfig) RepoDrv {
	switch cfg.RepoDrv() {
	case "InMemory":
		return newInMemoryRepoDrv(cfg)
	case "PgDB":
		return newPgRepoDrv(cfg)
	}
	slog.Error("Unknow repo driver",
		slog.String("RepoDrv", cfg.RepoDrv()),
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
	shards []inMemoryShard
}

func newInMemoryRepoDrv(cfg RepoDrvConfig) RepoDrv {
	shardSize := int(cfg.ShardSize())

	s := &InMemoryRepoDrv{
		shards: make([]inMemoryShard, shardSize),
	}

	for i := 0; i < shardSize; i++ {
		s.shards[i] = inMemoryShard{
			mu:    sync.RWMutex{},
			data:  make([]string, 0, 100),
			index: make(map[string]uint64, 100),
		}
	}

	slog.Info("Use InMemoryDrv")
	return s
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
func (s *InMemoryRepoDrv) UpSert(shardID byte, str string) (uint64, error) {
	if int(shardID) >= len(s.shards) {
		slog.Error("shard access out of bounds",
			slog.Uint64("received", uint64(shardID)),
			slog.Int("limit", len(s.shards)),
		)
		return 0, fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
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
		slog.Error("shard access out of bounds",
			slog.Uint64("received", uint64(shardID)),
			slog.Int("limit", len(s.shards)),
		)
		return fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	shard := &s.shards[shardID]

	shard.mu.Lock()
	if idx == uint64(len(shard.data)) {
		shard.data = append(shard.data, u)
	} else if idx > uint64(len(shard.data)) {
		newTail := make([]string, idx-uint64(len(shard.data))+1)
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
		return "", fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	shard := &s.shards[shardID]
	shard.mu.RLock()

	if idx >= uint64(len(shard.data)) {
		return "", fmt.Errorf("record not found: %d", idx)
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
func newPgRepoDrv(cfg RepoDrvConfig) RepoDrv {
	slog.Info("Use PgDrv")
	return newInMemoryRepoDrv(cfg)
}
