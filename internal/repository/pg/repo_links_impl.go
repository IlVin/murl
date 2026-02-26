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
	"murl/internal/repository/pgc/instance"
	"sync"

	"github.com/jackc/pgx/v5"
)

const sqlUpSert string = `
WITH ins AS (
    INSERT INTO murl (url) VALUES ($1)
    ON CONFLICT (url) DO NOTHING
    RETURNING id
)
SELECT id, 'f'::boolean AS conflict FROM ins
UNION ALL
SELECT id, 't'::boolean AS conflict FROM murl WHERE url = $1
LIMIT 1;
`

const sqlSet string = `
	INSERT INTO murl (id, url)
	VALUES ($1, $2)
	ON CONFLICT (id)
	DO UPDATE SET url = EXCLUDED.url;
`
const sqlSelect string = `
	SELECT url FROM murl
	WHERE id = $1
	LIMIT 1;
`

var ErrInternalServerError error = errors.New("internal server error")

type PgRepoLinks struct {
	cluster pgc.PgCluster
}

func NewPgRepoLinks(cluster pgc.PgCluster) repository.RepoLinks {
	return &PgRepoLinks{
		cluster: cluster,
	}
}

// UpSert записывает longURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
func (s *PgRepoLinks) UpSert(ctx context.Context, longURL string) (string, bool, error) {
	shardID := s.cluster.ShardID(longURL)
	shard, err := s.cluster.GetShard(shardID)
	if err != nil {
		return "", false, err
	}

	var idx uint64
	var cf bool

	errTx := shard.Tx(ctx, func(ctx context.Context, tx instance.PgxTxIface) error {
		return tx.QueryRow(ctx, sqlUpSert, longURL).Scan(&idx, &cf)
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

// Select получить по shortPath строке longURL строку
func (s *PgRepoLinks) Select(ctx context.Context, shortPath string) (string, error) {
	shardID, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return "", fmt.Errorf("invalid format shortPath: %w", err)
	}

	shard, err := s.cluster.GetShard(shardID)
	if err != nil {
		return "", fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, s.cluster.Size())
	}

	var longURL string
	err = shard.PgPool(ctx, func(ctx context.Context, p instance.PgxPoolIface) error {
		return p.QueryRow(ctx, sqlSelect, idx).Scan(&longURL)
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute query (%s): %w", sqlSelect, err)
	}

	return longURL, nil
}

func (s *PgRepoLinks) Set(ctx context.Context, longURL string, shortPath string) error {
	shardID, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return fmt.Errorf("invalid format shortPath: %w", err)
	}

	shard, err := s.cluster.GetShard(shardID)
	if err != nil {
		return fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, s.cluster.Size())
	}

	err = shard.PgPool(ctx, func(ctx context.Context, p instance.PgxPoolIface) error {
		_, err = p.Exec(ctx, sqlSet, idx, longURL)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", sqlSet, err)
	}

	return nil
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
type shardBatch struct {
	batch *pgx.Batch
	items []event.PayloadBatchItem
}

func (s *PgRepoLinks) BatchUpSert(ctx context.Context, batch event.PayloadBatch) event.PayloadBatch {
	// Группируем элементы батча по шардам
	byShardBatch := make(map[byte]*shardBatch)
	for i := range batch.Batch {
		sID := s.cluster.ShardID(batch.Batch[i].OriginalURL)
		if byShardBatch[sID] == nil {
			byShardBatch[sID] = &shardBatch{
				batch: &pgx.Batch{},
				items: make([]event.PayloadBatchItem, 0, len(batch.Batch)),
			}
		}
		byShardBatch[sID].items = append(byShardBatch[sID].items, batch.Batch[i])
		byShardBatch[sID].batch.Queue(sqlUpSert, batch.Batch[i].OriginalURL)
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
					if err := ctx.Err(); err != nil {
						return
					}
					b.items[i].ShortURL = ""
					b.items[i].ConflictFlag = false
					b.items[i].Err = ErrInternalServerError.Error()
					resCh <- b.items[i]
				}
				return
			}

			errPool := shard.Tx(ctx, func(ctx context.Context, tx instance.PgxTxIface) error {
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
				resCh <- b.items[i]
			}

		}(sID, b)
	}

	// Ждем джобы и закрываем результирующий канал
	go func() {
		wg.Wait()
		close(resCh)
	}()

	result := event.PayloadBatch{
		Batch: make([]event.PayloadBatchItem, 0, len(batch.Batch)),
	}
	for res := range resCh {
		result.Batch = append(result.Batch, res)
	}

	return result
}
