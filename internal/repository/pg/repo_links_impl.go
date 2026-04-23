// Package pg содержит реализацию репозиториев для работы с PostgreSQL.
package pg

import (
	"context"
	"errors"
	"fmt"
	"murl/internal/adapters/pgc"
	"murl/internal/dto"
	"murl/internal/model"
	"murl/internal/repository"
)

// ClusterSz определяет количество логических шардов в кластере БД.
const ClusterSz = 64

//go:generate $GOPATH/bin/mockgen                                   -destination=repo_links_pgx_mock_test.go         -package=$GOPACKAGE github.com/jackc/pgx/v5 Tx,Row,BatchResults
//go:generate $GOPATH/bin/mockgen -source=../../adapters/pgc/pgc.go -destination=repo_links_pg_instance_mock_test.go -package=$GOPACKAGE

// UpSertResult описывает структуру результата выполнения SQL-запроса UpSert.
type UpSertResult struct {
	Idx uint64
	Cf  bool
}

// sqlUpSert — типизированный SQL запрос, выполняющий вставку новой ссылки
// или получение ID существующей с использованием CTE.
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

// sqlSet — команда для принудительной вставки или обновления пары ID <-> URL.
var sqlSet = pgc.NewCommand(`
	INSERT INTO murl (id, url)
	VALUES ($1, $2)
	ON CONFLICT (id)
	DO UPDATE SET url = EXCLUDED.url
`,
).AsWrite()

// SelectResult описывает данные, возвращаемые при поиске оригинального URL.
type SelectResult struct {
	URL     string
	Deleted bool
}

// sqlSelect — запрос на получение оригинальной ссылки и её статуса (удалена или нет).
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

// PgRepoLinks реализует интерфейс repository.RepoLinks для PostgreSQL.
type PgRepoLinks struct {
	inst pgc.PgInstance
}

// NewPgRepoLinks — конструктор репозитория ссылок для Postgres.
func NewPgRepoLinks(inst pgc.PgInstance) repository.RepoLinks {
	return &PgRepoLinks{
		inst: inst,
	}
}

// UpSert записывает оригинальный URL в БД. Если такой URL уже есть, возвращает его ID
// и устанавливает conflictFlag в true.
func (s *PgRepoLinks) UpSert(ctx context.Context, originalURL string) (string, bool, error) {
	shardID := model.ShardID(originalURL, ClusterSz)
	res, err := pgc.FetchRow(ctx, s.inst, sqlUpSert, originalURL)
	if err != nil {
		return "", false, fmt.Errorf("failed to execute query (%s): %w", sqlUpSert.Name(), err)
	}
	shortPath, err := model.MakeShortPath(shardID, res.Idx)
	if err != nil {
		return "", false, fmt.Errorf("make short path fail: %w", err)
	}
	return shortPath, res.Cf, nil
}

// Select извлекает оригинальный URL по его короткому пути, декодируя ID из пути.
func (s *PgRepoLinks) Select(ctx context.Context, shortPath string) (string, bool, error) {
	_, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return "", false, fmt.Errorf("invalid format shortPath: %w", err)
	}

	res, err := pgc.FetchRow(ctx, s.inst, sqlSelect, idx)
	if err != nil {
		return "", false, fmt.Errorf("failed to execute query (%s): %w", sqlSelect.Name(), err)
	}
	return res.URL, res.Deleted, nil
}

// Set устанавливает жесткое соответствие ID и URL. Полезно для восстановления данных.
func (s *PgRepoLinks) Set(ctx context.Context, originalURL string, shortPath string) error {
	_, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return fmt.Errorf("invalid format shortPath: %w", err)
	}

	if _, err := pgc.Exec(ctx, s.inst, sqlSet, idx); err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", sqlSet.Name(), err)
	}

	return nil
}

// BatchUpSert выполняет массовую вставку ссылок через PgBatcher,
// обеспечивая высокую скорость за счет группировки запросов в один сетевой пакет.
func (s *PgRepoLinks) BatchUpSert(ctx context.Context, batch dto.Batch) dto.Batch {
	// Результат
	result := dto.Batch{
		Batch: make([]dto.BatchItem, 0, len(batch.Batch)),
	}

	batcherUpSert := pgc.NewPgBatcher[int](ctx, s.inst, sqlUpSert)

	// Запихиваем в батчер запросы в параллельной горутине
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
		err := func() error {
			if res.Err != nil {
				return res.Err
			}
			sID := model.ShardID(batch.Batch[res.Ctx].OriginalURL, ClusterSz)
			sPath, err := model.MakeShortPath(sID, res.Data.Idx)
			if err != nil {
				return err
			}
			result.Batch = append(result.Batch, dto.BatchItem{
				CorrelationID: batch.Batch[res.Ctx].CorrelationID,
				OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
				Err:           "",
				ShortURL:      sPath,
			})
			return nil
		}()
		// Обрабатываем ошибку конкретной записи
		if err != nil {
			result.Batch = append(result.Batch, dto.BatchItem{
				CorrelationID: batch.Batch[res.Ctx].CorrelationID,
				OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
				Err:           ErrInternalServerError.Error(),
				ShortURL:      "",
			})
			continue
		}
	}

	return result
}

// BatchSet выполняет пакетную установку соответствий URL-идентификаторов.
func (s *PgRepoLinks) BatchSet(ctx context.Context, batch dto.Batch) dto.Batch {
	// Результат
	result := dto.Batch{
		Batch: make([]dto.BatchItem, 0, len(batch.Batch)),
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
			result.Batch = append(result.Batch, dto.BatchItem{
				CorrelationID: batch.Batch[res.Ctx].CorrelationID,
				OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
				Err:           ErrInternalServerError.Error(),
				ShortURL:      "",
			})
			continue
		}
		result.Batch = append(result.Batch, dto.BatchItem{
			CorrelationID: batch.Batch[res.Ctx].CorrelationID,
			OriginalURL:   batch.Batch[res.Ctx].OriginalURL,
			Err:           "",
			ShortURL:      batch.Batch[res.Ctx].ShortURL,
		})
	}

	return result
}
