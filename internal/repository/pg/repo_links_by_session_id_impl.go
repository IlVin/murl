package pg

import (
	"context"
	"fmt"
	"log/slog"
	"murl/internal/model"
	"murl/internal/model/event"
	"murl/internal/repository"
	"murl/internal/repository/pgc"
	"sync"

	"github.com/jackc/pgx/v5"
)

const sqlUpSertBySessionID string = `
WITH ins AS (
    INSERT INTO murl (url, session_id) VALUES ($1, $2)
    ON CONFLICT (url) DO NOTHING
    RETURNING id
)
SELECT id, 'f'::boolean AS conflict FROM ins
UNION ALL
SELECT id, 't'::boolean AS conflict FROM murl WHERE url = $1
LIMIT 1;
`

const sqlDelBySessionID string = `
UPDATE murl
SET deleted = 't'::boolean
WHERE id = $1 AND session_id = $2
`

const sqlSetBySessionID string = `
	INSERT INTO murl (id, url, session_id)
	VALUES ($1, $2, $3)
	ON CONFLICT (id)
	DO UPDATE SET url = EXCLUDED.url, session_id = EXCLUDED.session_id;
`
const sqlSelectAllBySessionID string = `
	SELECT id, url FROM murl
	WHERE session_id = $1;
`

type PgRepoLinksBySessionID struct {
	cluster pgc.PgCluster
}

func NewPgRepoLinksBySessionID(cluster pgc.PgCluster) repository.RepoLinksBySessionID {
	return &PgRepoLinksBySessionID{
		cluster: cluster,
	}
}

// UpSert записывает originalURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
func (s *PgRepoLinksBySessionID) UpSert(ctx context.Context, sessionID string, originalURL string) (string, bool, error) {
	shardID := s.cluster.ShardID(sessionID)
	shard, err := s.cluster.GetShard(shardID)
	if err != nil {
		return "", false, err
	}

	var idx uint64
	var cf bool

	errTx := shard.Tx(ctx, func(ctx context.Context, tx pgc.PgxTxIface) error {
		return tx.QueryRow(ctx, sqlUpSertBySessionID, originalURL, sessionID).Scan(&idx, &cf)
	})
	if errTx != nil {
		return "", false, fmt.Errorf("failed to execute query (%s): %w", sqlUpSert, errTx)
	}
	shortPath, err := model.MakeShortPath(shardID, idx)
	if err != nil {
		return "", false, fmt.Errorf("make short path fail: %w", err)
	}
	return shortPath, cf, nil
}

func (s *PgRepoLinksBySessionID) SelectAll(ctx context.Context, sessionID string) ([]event.PayloadURLItem, error) {
	result := make([]event.PayloadURLItem, 0, 100)

	shardID := s.cluster.ShardID(sessionID)
	shard, err := s.cluster.GetShard(shardID)
	if err != nil {
		return result, err
	}

	err = shard.PgPool(ctx, func(ctx context.Context, p pgc.PgxPoolIface) error {
		//		return p.QueryRow(ctx, sqlSelectAllBySessionID, sessionID).Scan(&originalURL)
		rows, err := p.Query(ctx, sqlSelectAllBySessionID, sessionID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var idx uint64
			var originalURL string
			err := rows.Scan(&idx, &originalURL)
			if err != nil {
				return err
			}
			shortPath, err := model.MakeShortPath(shardID, idx)
			if err != nil {
				return err
			}
			result = append(result, event.PayloadURLItem{
				OriginalURL: originalURL,
				ShortURL:    shortPath,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to execute query (%s): %w", sqlSelect, err)
	}
	slog.Info("SelectAll", slog.Any("result", result))
	return result, nil
}

func (s *PgRepoLinksBySessionID) Set(ctx context.Context, sessionID string, originalURL string, shortPath string) error {
	shardID, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return fmt.Errorf("invalid format shortPath: %w", err)
	}

	shard, err := s.cluster.GetShard(shardID)
	if err != nil {
		return fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, s.cluster.Size())
	}

	err = shard.PgPool(ctx, func(ctx context.Context, p pgc.PgxPoolIface) error {
		_, err = p.Exec(ctx, sqlSetBySessionID, idx, originalURL, sessionID)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", sqlSet, err)
	}

	return nil
}

// BatchDeleteBySessionID - Создает отложенный батч на удаление URL по SessionID
func (s *PgRepoLinksBySessionID) BatchDelBySessionID(ctx context.Context, sessionID string, batch event.PayloadDeleteURLBySessionID) error {
	shardID := s.cluster.ShardID(sessionID)
	batcher, err := s.cluster.GetBatch(shardID)
	if err != nil {
		return err
	}

	for _, shortURL := range batch.ShortURLs {
		_, idx, err := model.ParseShortPath(shortURL)
		if err != nil {
			return fmt.Errorf("invalid format ShortURL (%s): %w", shortURL, err)
		}

		batcher.Add(sqlDelBySessionID, []any{idx, sessionID})
	}

	batcher.Flush()
	return nil
}

// TODO PgRepoLinksBySessionID.BatchUpSert нужно упростить, так как весть батч идет в один шард. Не нужно группировать и запускать параллельно несколько горутин
func (s *PgRepoLinksBySessionID) BatchUpSert(ctx context.Context, sessionID string, batch event.PayloadBatch) event.PayloadBatch {
	// Результат
	result := event.PayloadBatch{
		Batch: make([]event.PayloadBatchItem, 0, len(batch.Batch)),
	}

	// Группируем элементы батча по шардам
	byShardBatch := make(map[byte]*shardBatch)
	for i := range batch.Batch {
		// Ошибки сразу складываем в результат
		if len(batch.Batch[i].Err) > 0 {
			result.Batch = append(result.Batch, batch.Batch[i])
			continue
		}
		// Распределяем по шардам
		var sID byte
		var idx uint64
		var err error
		if len(batch.Batch[i].ShortURL) > 0 {
			// Режим Set
			sID, idx, err = model.ParseShortPath(batch.Batch[i].ShortURL)
			if err != nil {
				slog.Error("batch in mode 'Set' ShortPath parsing fail",
					slog.Any("err", err),
				)
				continue
			}
		} else {
			// Режим Add
			sID = s.cluster.ShardID(sessionID)
		}

		if byShardBatch[sID] == nil {
			byShardBatch[sID] = &shardBatch{
				batch: &pgx.Batch{},
				items: make([]event.PayloadBatchItem, 0, len(batch.Batch)),
			}
		}
		byShardBatch[sID].items = append(byShardBatch[sID].items, batch.Batch[i])

		if len(batch.Batch[i].ShortURL) > 0 {
			// Режим Set
			byShardBatch[sID].batch.Queue(sqlSetBySessionID, idx, batch.Batch[i].OriginalURL, sessionID)
		} else {
			// Режим Add
			byShardBatch[sID].batch.Queue(sqlUpSertBySessionID, batch.Batch[i].OriginalURL, sessionID)
		}
	}

	// Запускаем горутины
	resCh := make(chan event.PayloadBatchItem, len(batch.Batch))
	wg := sync.WaitGroup{}
	for sID, b := range byShardBatch {
		wg.Add(1)
		go func(sID byte, b *shardBatch) {
			defer wg.Done()

			shard, err := s.cluster.GetShard(sID)
			if err != nil {
				// Весь батч ошибочный
				slog.Error("shard access failed",
					slog.Any("err", err),
					slog.Int("sID", int(sID)),
				)
				for i := range b.items {
					b.items[i].ShortURL = ""
					b.items[i].ConflictFlag = false
					b.items[i].Err = ErrInternalServerError.Error()
					select {
					case resCh <- b.items[i]:
					case <-ctx.Done():
						return
					}
				}
				return
			}

			// Выполняем batch запрос к БД
			errPool := shard.Tx(ctx, func(ctx context.Context, tx pgc.PgxTxIface) error {
				br := tx.SendBatch(ctx, b.batch)
				defer br.Close()

				// Вычитываем все результаты
				for i := range b.items {
					if err := ctx.Err(); err != nil {
						return err
					}
					var idx uint64
					var cf bool
					if err := br.QueryRow().Scan(&idx, &cf); err != nil {
						return err
					}
					shortPath, err := model.MakeShortPath(sID, idx)
					if err != nil {
						return err
					}
					b.items[i].ConflictFlag = cf
					b.items[i].ShortURL = shortPath
				}
				return nil
			})

			// Весь батч ошибочный
			if errPool != nil {
				slog.Error("shard batch failed",
					slog.Any("err", errPool),
					slog.Int("sID", int(sID)),
					slog.Int("count", len(b.items)),
				)
				for i := range b.items {
					b.items[i].ShortURL = ""
					b.items[i].ConflictFlag = false
					b.items[i].Err = ErrInternalServerError.Error()
				}
			}

			// Пишем результат в канал
			for i := range b.items {
				if err := ctx.Err(); err != nil {
					return
				}
				select {
				case resCh <- b.items[i]:
				case <-ctx.Done():
					return
				}
			}

		}(sID, b)
	}

	// Ждем джобы и закрываем результирующий канал
	go func() {
		wg.Wait()
		close(resCh)
	}()

	for res := range resCh {
		result.Batch = append(result.Batch, res)
	}

	return result
}
