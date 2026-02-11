package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"murl/internal/model/event"
	"sync"

	"golang.org/x/sys/cpu"
)

// =============================================
//
//	InMemory
//
// =============================================
type inMemoryShard struct {
	mu      sync.RWMutex
	data    map[uint64]string
	index   map[string]uint64 // Для O(1) поиска при UpSert
	lastIdx uint64
	_       cpu.CacheLinePad
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
		s.shards[i].data = make(map[uint64]string, defaultCap)
		s.shards[i].index = make(map[string]uint64, defaultCap)
	}

	slog.Info("Use InMemoryDrv")
	return s
}

func (s *InMemoryRepoDrv) getShard(shardID byte) (*inMemoryShard, error) {
	if int(shardID) >= len(s.shards) {
		return nil, fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}
	return &s.shards[shardID], nil
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
func (s *InMemoryRepoDrv) UpSert(ctx context.Context, shardID byte, str string) (uint64, error) {
	shard, err := s.getShard(shardID)
	if err != nil {
		return 0, err
	}

	shard.mu.RLock()

	if shard.lastIdx == math.MaxUint64 {
		return 0, errors.New("lastIdx overflow")
	}
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
	idx := shard.lastIdx
	shard.lastIdx++
	shard.data[idx] = str
	shard.index[str] = idx
	shard.mu.Unlock()

	return idx, nil
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
func (s *InMemoryRepoDrv) BatchUpSert(ctx context.Context, batch []event.PayloadBatchItem) ([]event.PayloadBatchItem, error) {
	for i := 0; i < len(batch); i++ {
		if idx, err := s.UpSert(ctx, batch[i].ShardID, batch[i].OrigURL); err != nil {
			batch[i].Err = errInternalServerError
		} else {
			batch[i].Idx = idx
		}
	}

	return batch, nil
}

func (s *InMemoryRepoDrv) Set(ctx context.Context, shardID byte, idx uint64, u string) error {
	shard, err := s.getShard(shardID)
	if err != nil {
		return err
	}
	if idx == math.MaxUint64 {
		return errors.New("cannot set max uint64 index")
	}

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Синхронно обновляем и данные и индекс
	if val, ok := shard.data[idx]; ok {
		delete(shard.index, val)
	}

	if idx2, ok := shard.index[u]; ok {
		delete(shard.data, idx2)
	}
	shard.data[idx] = u
	shard.index[u] = idx
	if idx >= shard.lastIdx {
		shard.lastIdx = idx + 1
	}

	return nil
}

// По строке-идентификатору возвращает ранее записанную строку
func (s *InMemoryRepoDrv) Select(ctx context.Context, shardID byte, idx uint64) (string, error) {
	shard, err := s.getShard(shardID)
	if err != nil {
		return "", err
	}

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	if val, ok := shard.data[idx]; ok {
		return val, nil
	}

	return "", fmt.Errorf("record not found: %d", idx)
}

func (s *InMemoryRepoDrv) Ping(ctx context.Context) error {
	return nil
}
