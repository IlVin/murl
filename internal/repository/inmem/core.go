package inmem

import (
	"errors"
	"fmt"
	"murl/internal/model"
	"sync"

	"golang.org/x/sys/cpu"
)

// =============================================
//
//	InMemCore
//
// =============================================

type InMemConfig interface {
	ShardSize() byte
}

type InMemShard struct {
	_       sync.Locker // фиктивный интерфейс для go vet
	Mu      sync.RWMutex
	_       cpu.CacheLinePad
	Data    map[uint64]string
	Index   map[string]uint64
	LastIdx uint64
	_       cpu.CacheLinePad
}

type InMemCore struct {
	shards []InMemShard
}

func NewInMemCore(cfg InMemConfig) (*InMemCore, error) {
	shardSize := int(cfg.ShardSize())
	if shardSize <= 0 {
		return nil, errors.New("shard size must be greater than 0")
	}

	s := &InMemCore{
		shards: make([]InMemShard, shardSize),
	}

	for i := 0; i < shardSize; i++ {
		s.shards[i].Data = make(map[uint64]string, defaultCap)
		s.shards[i].Index = make(map[string]uint64, defaultCap)
	}

	return s, nil
}

func (c *InMemCore) Size() byte {
	return byte(len(c.shards))
}

// ShardID возвращает идентификатор шарда по ключу
func (c *InMemCore) ShardID(key any) byte {
	return model.ShardID(key, c.Size())
}

func (c *InMemCore) GetShard(shardID byte) (*InMemShard, error) {
	if shardID >= c.Size() {
		return nil, fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, c.Size())
	}
	return &c.shards[shardID], nil
}
