package inmem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// UpSert записывает originalURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
func (r *InMemRepoLinks) UpSert(ctx context.Context, originalURL string) (string, bool, error) {
	shardID := model.ShardID(originalURL, r.c.Size())

	shard, err := r.c.GetShard(shardID)
	if err != nil {
		return "", false, err
	}

	shard.Mu.RLock()

	if idx, ok := shard.Index[originalURL]; ok {
		shard.Mu.RUnlock()
		return makeShortPath(shardID, idx, true)
	}
	shard.Mu.RUnlock()

	shard.Mu.Lock()
	if shard.LastIdx == math.MaxUint64 {
		return "", false, errors.New("lastIdx overflow")
	}
	if idx, ok := shard.Index[originalURL]; ok {
		shard.Mu.Unlock()
		return makeShortPath(shardID, idx, true)
	}
	shard.LastIdx++ // Нумерация начинается с 1
	idx := shard.LastIdx
	shard.Data[idx] = originalURL
	shard.Index[originalURL] = idx
	shard.Mu.Unlock()

	return makeShortPath(shardID, idx, false)
}

// Select получить по shortPath строке originalURL строку
func (r *InMemRepoLinks) Select(ctx context.Context, shortPath string) (string, bool, error) {
	shardID, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return "", false, fmt.Errorf("invalid format shortPath: %w", err)
	}

	shard, err := r.c.GetShard(shardID)
	if err != nil {
		return "", false, fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, r.c.Size())
	}

	shard.Mu.RLock()
	defer shard.Mu.RUnlock()

	if originalURL, ok := shard.Data[idx]; ok {
		return originalURL, false, nil
	}

	return "", false, fmt.Errorf("record not found: %d", idx)
}

// Set установить жесткое соответствие originalURL -> shortPath
func (r *InMemRepoLinks) Set(ctx context.Context, originalURL string, shortPath string) error {
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

	if idx2, ok := shard.Index[originalURL]; ok {
		delete(shard.Data, idx2)
	}
	shard.Data[idx] = originalURL
	shard.Index[originalURL] = idx
	if idx >= shard.LastIdx {
		shard.LastIdx = idx
	}

	return nil
}

// BatchUpSert пакетная установка URL
func (r *InMemRepoLinks) BatchUpSert(ctx context.Context, b event.PayloadBatch) event.PayloadBatch {
	for i := range b.Batch {
		if len(b.Batch[i].Err) > 0 {
			continue
		}
		if ctx.Err() != nil {
			b.Batch[i].Err = ErrInternalServerError.Error()
			continue
		}
		if len(b.Batch[i].ShortURL) > 0 {
			// Режим Set для проигрывания WAL
			if err := r.Set(ctx, b.Batch[i].OriginalURL, b.Batch[i].ShortURL); err != nil {
				slog.Error("batch set fail",
					slog.String("OriginalURL", b.Batch[i].OriginalURL),
					slog.String("ShortURL", b.Batch[i].ShortURL),
					slog.Any("err", err),
				)
			}
		} else {
			// Режим Add
			if shortURL, cf, err := r.UpSert(ctx, b.Batch[i].OriginalURL); err != nil {
				slog.Error("batch upsert fail",
					slog.String("OriginalURL", b.Batch[i].OriginalURL),
					slog.Any("err", err),
				)
				b.Batch[i].Err = ErrInternalServerError.Error()
			} else {
				b.Batch[i].ShortURL = shortURL
				b.Batch[i].ConflictFlag = cf
			}
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
