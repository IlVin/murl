package pg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/adapters/pgc"
	"murl/internal/model"
	"murl/internal/model/event"
	"murl/internal/repository"

	"github.com/jackc/pgx/v5"
)

const ClusterSz = 64

//go:generate $GOPATH/bin/mockgen                                   -destination=repo_links_pgx_mock_test.go         -package=$GOPACKAGE github.com/jackc/pgx/v5 Tx,Row,BatchResults
//go:generate $GOPATH/bin/mockgen -source=../../adapters/pgc/pgc.go -destination=repo_links_pg_instance_mock_test.go -package=$GOPACKAGE

type UpSertResult struct {
	Idx uint64
	Cf  bool
}

// Описываем маппинг полей вручную для скорости и типобезопасности
var sqlUpSert = pgc.NewQuery(`
WITH ins AS (
    INSERT INTO murl (url)
    VALUES ($1)
    ON CONFLICT (url) DO NOTHING
    RETURNING id
)
(
    SELECT id, 'f'::boolean AS conflict
    FROM ins
) UNION ALL (
    SELECT id, 't'::boolean AS conflict
    FROM murl
    WHERE url = $1
)
LIMIT 1
`,
	func(u *UpSertResult) []any {
		return []any{&u.Idx, &u.Cf}
	},
).AsRead()

var sqlSet = pgc.NewCommand(`
	INSERT INTO murl (id, url)
	VALUES ($1, $2)
	ON CONFLICT (id)
	DO UPDATE SET url = EXCLUDED.url
`,
).AsWrite()

type SelectResult struct {
	URL     string
	Deleted bool
}

var sqlSelect = pgc.NewQuery(`
	SELECT url, deleted FROM murl
	WHERE id = $1
	LIMIT 1;
`,
	func(u *SelectResult) []any {
		return []any{&u.URL, &u.Deleted}
	},
).AsRead()

var ErrInternalServerError error = errors.New("internal server error")

type PgRepoLinks struct {
	inst pgc.PgInstance
}

func NewPgRepoLinks(inst pgc.PgInstance) repository.RepoLinks {
	return &PgRepoLinks{
		inst: inst,
	}
}

// UpSert записывает originalURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
func (s *PgRepoLinks) UpSert(ctx context.Context, originalURL string) (string, bool, error) {
	slog.Info("UpSert",
		slog.String("originalURL", originalURL),
	)
	shardID := model.ShardID(originalURL, ClusterSz)
	res, err := pgc.FetchRow(ctx, s.inst, sqlUpSert, originalURL)
	slog.Info("UpSert res",
		slog.Any("res", res),
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to execute query (%s): %w", sqlUpSert, err)
	}
	shortPath, err := model.MakeShortPath(shardID, res.Idx)
	if err != nil {
		return "", false, fmt.Errorf("make short path fail: %w", err)
	}
	slog.Info("UpSert shortPath",
		slog.Any("shortPath", shortPath),
	)
	return shortPath, res.Cf, nil
}

// Select получить по shortPath строке originalURL строку
func (s *PgRepoLinks) Select(ctx context.Context, shortPath string) (string, bool, error) {
	_, idx, err := model.ParseShortPath(shortPath)
	slog.Info("SELECT",
		slog.Any("shortPath", shortPath),
		slog.Any("idx", idx),
		slog.Any("err", err),
	)
	if err != nil {
		return "", false, fmt.Errorf("invalid format shortPath: %w", err)
	}

	res, err := pgc.FetchRow(ctx, s.inst, sqlSelect, idx)
	slog.Info("SELECT res",
		slog.Any("res", res),
		slog.Any("err", err),
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to execute query (%s): %w", sqlSelect, err)
	}
	return res.URL, res.Deleted, nil
}

func (s *PgRepoLinks) Set(ctx context.Context, originalURL string, shortPath string) error {
	slog.Info("Set",
		slog.String("originalURL", originalURL),
		slog.String("shortPath", shortPath),
	)
	_, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return fmt.Errorf("invalid format shortPath: %w", err)
	}

	if _, err := pgc.Exec(ctx, s.inst, sqlSet, idx); err != nil {
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
	slog.Info("BatchUpSert",
		slog.Any("batch", batch),
	)
	// Результат
	result := event.PayloadBatch{
		Batch: make([]event.PayloadBatchItem, 0, len(batch.Batch)),
	}

	// Запихиваем в батчер запросы в параллельной горутине
	batcherUpSert := pgc.NewPgBatcher[int](ctx, s.inst, sqlUpSert)

	go func() {
		defer batcherUpSert.Close()

		for i := range batch.Batch {
			// Запихиваем запрос в batch
			batcherUpSert.Requests() <- pgc.BatchEntry[int]{
				Args: []any{batch.Batch[i].OriginalURL},
				Ctx:  i, // Прокидываем индекс как контекст
			}
		}
	}()

	for res := range batcherUpSert.Results() {
		// Обрабатываем ошибку конкретной записи
		if res.Err != nil {
			result.Batch = append(result.Batch, event.PayloadBatchItem{
				CorrelationID: batch.Batch[res.Ctx].CorrelationID,
				OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
				Err:           ErrInternalServerError.Error(),
				ShortURL:      "",
			})
			continue
		}
		sID := model.ShardID(batch.Batch[res.Ctx].OriginalURL, ClusterSz)
		sPath, err := model.MakeShortPath(sID, res.Data.Idx)
		if err != nil {
			result.Batch = append(result.Batch, event.PayloadBatchItem{
				CorrelationID: batch.Batch[res.Ctx].CorrelationID,
				OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
				Err:           ErrInternalServerError.Error(),
				ShortURL:      "",
			})
			continue
		}
		result.Batch = append(result.Batch, event.PayloadBatchItem{
			CorrelationID: batch.Batch[res.Ctx].CorrelationID,
			OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
			Err:           "",
			ShortURL:      sPath,
		})
	}
	slog.Info("batch result",
		slog.Any("result", result),
	)

	return result
}

func (s *PgRepoLinks) BatchSet(ctx context.Context, batch event.PayloadBatch) event.PayloadBatch {
	// Результат
	result := event.PayloadBatch{
		Batch: make([]event.PayloadBatchItem, 0, len(batch.Batch)),
	}

	// Запихиваем в батчер запросы в параллельной горутине
	batcherSet := pgc.NewPgBatcher[int](ctx, s.inst, sqlSet)

	go func() {
		defer batcherSet.Close()

		for i := range batch.Batch {
			_, idx, err := model.ParseShortPath(batch.Batch[i].ShortURL)
			if err != nil {
				continue
			}

			// Запихиваем запрос в batch
			batcherSet.Requests() <- pgc.BatchEntry[int]{
				Args: []any{idx, batch.Batch[i].OriginalURL},
				Ctx:  i, // Прокидываем индекс как контекст
			}
		}
	}()

	for res := range batcherSet.Results() {
		// Обрабатываем ошибку конкретной записи
		if res.Err != nil {
			result.Batch = append(result.Batch, event.PayloadBatchItem{
				CorrelationID: batch.Batch[res.Ctx].CorrelationID,
				OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
				Err:           ErrInternalServerError.Error(),
				ShortURL:      "",
			})
			continue
		}
		result.Batch = append(result.Batch, event.PayloadBatchItem{
			CorrelationID: batch.Batch[res.Ctx].CorrelationID,
			OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
			Err:           "",
			ShortURL:      batch.Batch[res.Ctx].ShortURL,
		})
	}

	return result
}
