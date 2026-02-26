package inmem

import (
	"context"
	"errors"
	"fmt"
	"math"
	"murl/internal/model"
	"murl/internal/model/event"
	"murl/internal/repository"
)

var ErrInternalServerError error = errors.New("internal server error")

type InMemRepoLinks struct {
	c *InMemCore
}

const defaultCap int = 300

func NewInMemRepoLinks(core *InMemCore) repository.RepoLinks {
	r := &InMemRepoLinks{
		c: core,
	}
	return r
}

// UpSert записывает longURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
func (r *InMemRepoLinks) UpSert(ctx context.Context, longURL string) (string, bool, error) {
	shardID := model.ShardID(longURL, r.c.Size())

	shard, err := r.c.GetShard(shardID)
	if err != nil {
		return "", false, err
	}

	shard.Mu.RLock()

	if idx, ok := shard.Index[longURL]; ok {
		shard.Mu.RUnlock()
		return makeShortPath(shardID, idx, true)
	}
	shard.Mu.RUnlock()

	shard.Mu.Lock()
	if shard.LastIdx == math.MaxUint64 {
		return "", false, errors.New("lastIdx overflow")
	}
	if idx, ok := shard.Index[longURL]; ok {
		shard.Mu.Unlock()
		return makeShortPath(shardID, idx, true)
	}
	shard.LastIdx++ // Нумерация начинается с 1
	idx := shard.LastIdx
	shard.Data[idx] = longURL
	shard.Index[longURL] = idx
	shard.Mu.Unlock()

	return makeShortPath(shardID, idx, false)
}

// Select получить по shortPath строке longURL строку
func (r *InMemRepoLinks) Select(ctx context.Context, shortPath string) (string, error) {
	shardID, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return "", fmt.Errorf("invalid format shortPath: %w", err)
	}

	shard, err := r.c.GetShard(shardID)
	if err != nil {
		return "", fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, r.c.Size())
	}

	shard.Mu.RLock()
	defer shard.Mu.RUnlock()

	if longURL, ok := shard.Data[idx]; ok {
		return longURL, nil
	}

	return "", fmt.Errorf("record not found: %d", idx)
}

// Set установить жесткое соответствие longURL -> shortPath
func (r *InMemRepoLinks) Set(ctx context.Context, longURL string, shortPath string) error {
	shardID, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return fmt.Errorf("invalid format shortPath: %w", err)
	}

	shard, err := r.c.GetShard(shardID)
	if err != nil {
		return err
	}
	if idx == math.MaxUint64 {
		return errors.New("cannot set max uint64 index")
	}

	shard.Mu.Lock()
	defer shard.Mu.Unlock()

	// Синхронно обновляем и данные и индекс
	if val, ok := shard.Data[idx]; ok {
		delete(shard.Index, val)
	}

	if idx2, ok := shard.Index[longURL]; ok {
		delete(shard.Data, idx2)
	}
	shard.Data[idx] = longURL
	shard.Index[longURL] = idx
	if idx >= shard.LastIdx {
		shard.LastIdx = idx
	}

	return nil
}

// BatchUpSert пакетная установка URL
func (r *InMemRepoLinks) BatchUpSert(ctx context.Context, b event.PayloadBatch) event.PayloadBatch {
	for i := range b.Batch {
		if ctx.Err() != nil {
			b.Batch[i].Err = ErrInternalServerError.Error()
			continue
		}
		if ShortPath, cf, err := r.UpSert(ctx, b.Batch[i].OriginalURL); err != nil {
			b.Batch[i].Err = ErrInternalServerError.Error()
		} else {
			b.Batch[i].ShortURL = ShortPath
			b.Batch[i].ConflictFlag = cf
		}
	}

	return b
}

func makeShortPath(shardID byte, idx uint64, conflictFlag bool) (string, bool, error) {
	shortPath, err := model.MakeShortPath(shardID, idx)
	if err != nil {
		return shortPath, conflictFlag, fmt.Errorf("make short path fail: %w", err)
	}
	return shortPath, conflictFlag, nil
}
