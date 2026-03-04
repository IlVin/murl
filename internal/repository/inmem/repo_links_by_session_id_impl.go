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

type InMemRepoLinksBySessionID struct {
	c *InMemCore
}

func NewInMemRepoLinksBySessionID(core *InMemCore) repository.RepoLinksBySessionID {
	r := &InMemRepoLinksBySessionID{
		c: core,
	}
	return r
}

// UpSert записывает originalURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
func (r *InMemRepoLinksBySessionID) UpSert(ctx context.Context, sessionID string, originalURL string) (string, bool, error) {
	shardID := model.ShardID(sessionID, r.c.Size())

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
	if data, ok := shard.Sessions[sessionID]; ok {
		shard.Sessions[sessionID] = append(data, idx)
	} else {
		slog.Info("UpSertBySessionID",
			slog.Uint64("idx", idx),
			slog.Uint64("shardID", uint64(shardID)),
			slog.String("sessionID", sessionID),
		)
		shard.Sessions[sessionID] = append(make([]uint64, 0, 8), idx)
	}

	shard.Mu.Unlock()
	return makeShortPath(shardID, idx, false)
}

// SelectAll получить по sessionID строке все URL в формате [{"short_url": "http://...","original_url": "http://..."},...]
func (r *InMemRepoLinksBySessionID) SelectAll(ctx context.Context, sessionID string) ([]event.PayloadURLItem, error) {
	result := make([]event.PayloadURLItem, 0, 100)

	shardID := model.ShardID(sessionID, r.c.Size())
	shard, err := r.c.GetShard(shardID)
	if err != nil {
		return result, fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, r.c.Size())
	}

	shard.Mu.RLock()
	defer shard.Mu.RUnlock()

	data, ok := shard.Sessions[sessionID]
	if !ok {
		return result, nil
	}

	for i := range data {
		if originalURL, ok := shard.Data[data[i]]; ok {
			shortPath, err := model.MakeShortPath(shardID, data[i])
			if err == nil {
				result = append(result, event.PayloadURLItem{
					OriginalURL: originalURL,
					ShortURL:    shortPath,
				})
			} else {
				slog.Error("operation MakeShortPath fail",
					slog.Uint64("shardID", uint64(shardID)),
					slog.Uint64("idx", data[i]),
					slog.Any("err", err),
				)
			}
		} else {
			slog.Error("lookup failed: reference idx does not exist in shard.Data",
				slog.String("SessionID", sessionID),
				slog.Uint64("idx", data[i]),
			)
		}
	}

	return result, nil
}

// Set установить жесткое соответствие originalURL -> shortPath
func (r *InMemRepoLinksBySessionID) Set(ctx context.Context, sessionID string, originalURL string, shortPath string) error {
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
	if data, ok := shard.Sessions[sessionID]; ok {
		shard.Sessions[sessionID] = append(data, idx)
	} else {
		shard.Sessions[sessionID] = append(make([]uint64, 0, 8), idx)
	}
	if idx >= shard.LastIdx {
		shard.LastIdx = idx
	}

	return nil
}

// BatchDeleteBySessionID - Создает отложенный батч на удаление URL по SessionID
func (r *InMemRepoLinksBySessionID) BatchDelBySessionID(ctx context.Context, sessionID string, batch event.PayloadDeleteURLBySessionID) error {
	slog.Error("BatchDelBySessionID is not implemented")
	return nil
}

// BatchUpSert пакетная установка URL
func (r *InMemRepoLinksBySessionID) BatchUpSert(ctx context.Context, sessionID string, b event.PayloadBatch) event.PayloadBatch {
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
			if err := r.Set(ctx, sessionID, b.Batch[i].OriginalURL, b.Batch[i].ShortURL); err != nil {
				slog.Error("batch set fail",
					slog.String("OriginalURL", b.Batch[i].OriginalURL),
					slog.String("ShortURL", b.Batch[i].ShortURL),
					slog.Any("err", err),
				)
			}
		} else {
			// Режим Add
			if shortURL, cf, err := r.UpSert(ctx, sessionID, b.Batch[i].OriginalURL); err != nil {
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
