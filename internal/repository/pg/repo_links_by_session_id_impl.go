package pg

import (
	"context"
	"fmt"
	"log/slog"
	"murl/internal/adapters/pgc"
	"murl/internal/model"
	"murl/internal/model/event"
	"murl/internal/repository"
)

type UpSertBySessIDResult struct {
	Idx uint64
	Cf  bool
}

var sqlUpSertBySessionID = pgc.NewQuery(`
	WITH ins AS (
		INSERT INTO murl (url, session_id) VALUES ($1, $2)
		ON CONFLICT (url) DO NOTHING
		RETURNING id
	)
	SELECT id, 'f'::boolean AS conflict FROM ins
	UNION ALL
	SELECT id, 't'::boolean AS conflict FROM murl WHERE url = $1
	LIMIT 1;
`,
	func(u *UpSertBySessIDResult) []any {
		return []any{&u.Idx, &u.Cf}
	},
).AsWrite()

var sqlDelBySessionID = pgc.NewCommand(`
	UPDATE murl
	SET deleted = 't'::boolean
	WHERE id = $1 AND session_id = $2
`).AsWrite()

var sqlSetBySessionID = pgc.NewCommand(`
	INSERT INTO murl (id, url, session_id)
	VALUES ($1, $2, $3)
	ON CONFLICT (id)
	DO UPDATE SET url = EXCLUDED.url, session_id = EXCLUDED.session_id;
`).AsWrite()

type SelectAllBySessionIDResult struct {
	Idx         uint64
	OriginalURL string
}

var sqlSelectAllBySessionID = pgc.NewQuery(`
	SELECT id, url FROM murl
	WHERE session_id = $1;
`,
	func(u *SelectAllBySessionIDResult) []any {
		return []any{&u.Idx, &u.OriginalURL}
	},
).AsRead()

type PgRepoLinksBySessionID struct {
	inst pgc.PgInstance
}

func NewPgRepoLinksBySessionID(inst pgc.PgInstance) repository.RepoLinksBySessionID {
	return &PgRepoLinksBySessionID{
		inst: inst,
	}
}

// UpSert записывает originalURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
func (s *PgRepoLinksBySessionID) UpSert(ctx context.Context, sessionID string, originalURL string) (string, bool, error) {
	shardID := model.ShardID(sessionID, ClusterSz)

	res, err := pgc.FetchRow(ctx, s.inst, sqlUpSertBySessionID, originalURL, sessionID)
	if err != nil {
		return "", false, fmt.Errorf("failed to execute query (%s): %w", sqlUpSertBySessionID, err)
	}
	shortPath, err := model.MakeShortPath(shardID, res.Idx)
	if err != nil {
		return "", false, fmt.Errorf("make short path fail: %w", err)
	}
	return shortPath, res.Cf, nil
}

func (s *PgRepoLinksBySessionID) SelectAll(ctx context.Context, sessionID string) ([]event.PayloadURLItem, error) {
	result := make([]event.PayloadURLItem, 0, 100)

	shardID := model.ShardID(sessionID, ClusterSz)

	for res, err := range pgc.Fetch(ctx, s.inst, sqlSelectAllBySessionID, sessionID) {
		if err != nil {
			return result, fmt.Errorf("failed to execute query (%s): %w", sqlSelectAllBySessionID, err)
		}
		shortPath, err := model.MakeShortPath(shardID, res.Idx)
		if err != nil {
			return result, err
		}
		result = append(result, event.PayloadURLItem{
			OriginalURL: res.OriginalURL,
			ShortURL:    shortPath,
		})
	}

	slog.Info("SelectAll", slog.Any("result", result))
	return result, nil
}

func (s *PgRepoLinksBySessionID) Set(ctx context.Context, sessionID string, originalURL string, shortPath string) error {
	_, idx, err := model.ParseShortPath(shortPath)
	if err != nil {
		return fmt.Errorf("invalid format shortPath: %w", err)
	}

	if _, err := pgc.Exec(ctx, s.inst, sqlSetBySessionID, idx, originalURL, sessionID); err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", sqlSetBySessionID, err)
	}

	return nil
}

// BatchDeleteBySessionID - Создает отложенный батч на удаление URL по SessionID
func (s *PgRepoLinksBySessionID) BatchDelBySessionID(ctx context.Context, sessionID string, batch event.PayloadDeleteURLBySessionID) error {

	// Фоновый батчер
	batcherDelBySessionID := pgc.NewPgBatcher[int](context.WithoutCancel(ctx), s.inst, sqlDelBySessionID)

	// Запихиваем в батчер запросы в параллельной горутине
	go func() {
		defer batcherDelBySessionID.Close()

		for i := range batch.ShortURLs {
			shortURL := batch.ShortURLs[i]
			_, idx, err := model.ParseShortPath(shortURL)
			if err != nil {
				continue
			}

			batcherDelBySessionID.Requests() <- pgc.BatchEntry[int]{
				Args: []any{idx, sessionID},
				Ctx:  i, // Прокидываем индекс как контекст
			}
		}
	}()

	return nil
}

func (s *PgRepoLinksBySessionID) BatchUpSert(ctx context.Context, sessionID string, batch event.PayloadBatch) (event.PayloadBatch, error) {
	if len(batch.Batch) == 0 {
		return batch, nil
	}

	sID := model.ShardID(sessionID, ClusterSz)

	batcherUpSert := pgc.NewPgBatcher[int](ctx, s.inst, sqlUpSertBySessionID)
	// Запихиваем в батчер запросы в параллельной горутине
	go func() {
		defer batcherUpSert.Close()

		for i := range batch.Batch {
			batcherUpSert.Requests() <- pgc.BatchEntry[int]{
				Args: []any{batch.Batch[i].OriginalURL, sessionID},
				Ctx:  i, // Прокидываем индекс как контекст
			}
		}
	}()

	// Собираем результаты
	for res := range batcherUpSert.Results() {
		i := res.Ctx
		// Обрабатываем ошибку конкретной записи
		if res.Err != nil {
			batch.Batch[i].Err = ErrInternalServerError.Error()
			continue
		}
		sPath, err := model.MakeShortPath(sID, res.Data.Idx)
		if err != nil {
			batch.Batch[i].Err = ErrInternalServerError.Error()
			continue
		}
		batch.Batch[i].ShortURL = sPath
	}

	// Ждем когда батчер отработает
	<-batcherUpSert.Results()

	return batch, nil
}

func (s *PgRepoLinksBySessionID) BatchSet(ctx context.Context, sessionID string, batch event.PayloadBatch) (event.PayloadBatch, error) {
	if len(batch.Batch) == 0 {
		return batch, nil
	}

	batcherSet := pgc.NewPgBatcher[int](ctx, s.inst, sqlSetBySessionID)
	// Запихиваем в батчер запросы в параллельной горутине
	go func() {
		defer batcherSet.Close()

		for i := range batch.Batch {
			_, idx, err := model.ParseShortPath(batch.Batch[i].ShortURL)
			if err != nil {
				batch.Batch[i].Err = err.Error()
				continue
			}
			batcherSet.Requests() <- pgc.BatchEntry[int]{
				Args: []any{idx, batch.Batch[i].OriginalURL, sessionID},
				Ctx:  i, // Прокидываем индекс как контекст
			}
		}
	}()

	// Ждем когда батчер отработает
	<-batcherSet.Results()

	return batch, nil
}
