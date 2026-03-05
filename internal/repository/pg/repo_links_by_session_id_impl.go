package pg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/model"
	"murl/internal/model/event"
	"murl/internal/repository"
	"murl/internal/repository/pgc"

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
		return "", false, fmt.Errorf("failed to execute query (%s): %w", sqlUpSertBySessionID, errTx)
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
		return nil, fmt.Errorf("failed to execute query (%s): %w", sqlSelectAllBySessionID, err)
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
		return fmt.Errorf("failed to execute query (%s): %w", sqlSetBySessionID, err)
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

	return nil
}

func (s *PgRepoLinksBySessionID) BatchUpSert(ctx context.Context, sessionID string, batch event.PayloadBatch) (event.PayloadBatch, error) {
	if len(batch.Batch) == 0 {
		return batch, nil
	}

	// 1. Все записи одной сессии гарантированно попадают в один шард
	shardID := s.cluster.ShardID(sessionID)
	shard, err := s.cluster.GetShard(shardID)
	if err != nil {
		for i := range batch.Batch {
			if batch.Batch[i].Err != "" {
				batch.Batch[i].Err = errors.Join(errors.New(batch.Batch[i].Err), err).Error()
			} else {
				batch.Batch[i].Err = err.Error()
			}
		}
		return batch, fmt.Errorf("get shard fail: %w", err)
	}

	// 2. Подготавливаем нативный pgx батч
	pgxBatch := &pgx.Batch{}
	for i := range batch.Batch {
		if batch.Batch[i].ShortURL != "" {
			// Режим Set
			_, idx, err := model.ParseShortPath(batch.Batch[i].ShortURL)
			if err != nil {
				// Если формат плохой, помечаем ошибкой и пропускаем
				batch.Batch[i].Err = err.Error()
				continue
			}
			pgxBatch.Queue(sqlSetBySessionID, idx, batch.Batch[i].OriginalURL, sessionID)
		} else {
			// Режим Add
			pgxBatch.Queue(sqlUpSertBySessionID, batch.Batch[i].OriginalURL, sessionID)
		}
	}

	// 3. Выполняем батч в транзакции
	errTx := shard.Tx(ctx, func(ctx context.Context, tx pgc.PgxTxIface) error {
		br := tx.SendBatch(ctx, pgxBatch)
		defer br.Close()

		for i := range batch.Batch {
			// Пропускаем те, что уже с ошибкой парсинга
			if batch.Batch[i].Err != "" {
				continue
			}

			if batch.Batch[i].ShortURL == "" {
				// Читаем результат для sqlUpSertBySessionID (id, conflict)
				var idx uint64
				var cf bool
				if err := br.QueryRow().Scan(&idx, &cf); err != nil {
					return fmt.Errorf("scan upsert result fail (index %d): %w", i, err)
				}

				shortPath, err := model.MakeShortPath(shardID, idx)
				if err != nil {
					return fmt.Errorf("make short path fail: %w", err)
				}
				batch.Batch[i].ShortURL = shortPath
				batch.Batch[i].ConflictFlag = cf
			} else {
				// Для sqlSetBySessionID просто проверяем выполнение
				if _, err := br.Exec(); err != nil {
					return fmt.Errorf("exec set fail (index %d): %w", i, err)
				}
			}
		}
		return nil
	})

	if errTx != nil {
		for i := range batch.Batch {
			if batch.Batch[i].Err != "" {
				batch.Batch[i].Err = errTx.Error()
			}
		}
		return batch, fmt.Errorf("batch execution failed: %w", errTx)
	}

	return batch, nil
}
