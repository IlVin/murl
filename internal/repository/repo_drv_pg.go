package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/model/event"
	pgc "murl/internal/repository/pgc"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sqlUpSert string = `
WITH ins AS (
    INSERT INTO murl (url) VALUES ($1)
    ON CONFLICT (url) DO NOTHING
    RETURNING id
)
SELECT id, 0 AS conflict FROM ins
UNION ALL
SELECT id, 1 AS conflict FROM murl WHERE url = $1
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

// =============================================
//
//	PgDrv
//
// =============================================

//go:generate mockgen -source=$GOFILE -destination=repo_drv_pg_mocks_test.go -package=$GOPACKAGE

// Чтобы протестировать драйвер постгреса, приходится городить интерфейс
// DBHandler описывает методы pgc.PgHndl для мокирования в тестах
type DBHandler interface {
	Tx(ctx context.Context, cb func(ctx context.Context, tx pgx.Tx) (any, error)) (any, error)
	PgPool(ctx context.Context, cb func(ctx context.Context, p *pgxpool.Pool) error) error
	Ping(ctx context.Context) error
	Instance() string
	RunMigrations(context.Context) error
}

type PgRepoDrv struct {
	shards []DBHandler
}

func newPgRepoDrv(ctx context.Context, cfg RepoDrvConfig) (RepoDrv, error) {
	slog.Info("Use PgDrv")
	shardSize := int(cfg.ShardSize())

	r := &PgRepoDrv{
		shards: make([]DBHandler, shardSize),
	}

	// В БД на текущий момент шардирования нет,
	// но ожидается горизонтальное масштабирование.
	// Поэтому в драйвере реализуем абстракцию шардирования, но на одной БД
	pgHndl, err := pgc.NewPgHndl(ctx, "PgRepoDrv", cfg.DBDSN())
	if err != nil {
		return nil, fmt.Errorf("failed create PgRepoDrv: %w", err)
	}

	for i := 0; i < shardSize; i++ {
		r.shards[i] = pgHndl
	}

	slog.Info("Use PgRepoDrv")

	return r, nil
}

func (s *PgRepoDrv) RunMigrations(ctx context.Context) error {
	// Собираем уникальные инстансы
	instances := make(map[string]DBHandler)
	for i, shard := range s.shards {
		instances[shard.Instance()] = s.shards[i]
	}

	// На каждом инстансе запускаем миграцию
	errs := make([]error, 0, 10)
	for instance, shard := range instances {
		select {
		case <-ctx.Done():
			errs = append(errs, errors.New("migration interrupted"))
			return errors.Join(errs...)
		default:
			slog.Info("Run migration",
				slog.String("instance", instance),
			)
			errs = append(errs, shard.RunMigrations(ctx))
		}
	}
	return errors.Join(errs...)
}

func (s *PgRepoDrv) getShard(shardID byte) (DBHandler, error) {
	if int(shardID) >= len(s.shards) {
		return nil, fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}
	return s.shards[shardID], nil
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
type upsertResult struct {
	ID uint64
	CF int
}

func (s *PgRepoDrv) UpSert(ctx context.Context, shardID byte, str string) (uint64, error) {
	shard, err := s.getShard(shardID)
	if err != nil {
		return 0, err
	}

	res, err := shard.Tx(ctx, func(ctx context.Context, tx pgx.Tx) (any, error) {
		var id uint64
		var cf int
		err := tx.QueryRow(ctx, sqlUpSert, str).Scan(&id, &cf)
		return upsertResult{ID: id, CF: cf}, err
	})
	if err != nil {
		return 0, fmt.Errorf("failed to execute query (%s): %w", sqlUpSert, err)
	}

	val, ok := res.(upsertResult)
	if !ok {
		return 0, errors.New("unexpected return type")
	}

	if val.CF != 0 {
		return val.ID, ErrRecordAlreadyExists
	}

	return val.ID, nil
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
func (s *PgRepoDrv) BatchUpSert(ctx context.Context, batch []event.PayloadBatchItem) ([]event.PayloadBatchItem, error) {
	// Группируем элементы батча по шардам
	byShard := make(map[byte][]int, len(s.shards))
	for i, b := range batch {
		_, err := s.getShard(b.ShardID)
		if err != nil {
			return nil, fmt.Errorf("shardID [%d] out of range [0, .., %d]: %v", b.ShardID, len(s.shards)-1, b)
		}
		if byShard[b.ShardID] == nil {
			byShard[b.ShardID] = make([]int, 0, defaultCap)
		}
		byShard[b.ShardID] = append(byShard[b.ShardID], i)
	}

	// Группа параллельно работающих горутин, возвращающих ошибки
	wg := sync.WaitGroup{}

	// Запускаем горутины
	for sID, items := range byShard {
		wg.Add(1)
		go func(items []int, shard DBHandler) {
			defer wg.Done()
			_, batchErr := shard.Tx(ctx, func(ctx context.Context, tx pgx.Tx) (any, error) {
				b := &pgx.Batch{}
				for _, i := range items {
					b.Queue(sqlUpSert, batch[i].OrigURL)
				}
				br := tx.SendBatch(ctx, b)
				defer br.Close()

				for _, i := range items {
					var idx uint64
					var cf int
					if err := br.QueryRow().Scan(&idx, &cf); err != nil {
						return nil, fmt.Errorf("failed to execute query (%s): %w", sqlUpSert, err)
					}
					batch[i].Idx = idx
				}

				return nil, nil
			})

			if batchErr != nil {
				slog.Error("failed to execute shard batch",
					slog.Int("ShardID", int(sID)),
					slog.Any("err", batchErr),
				)
				// Транзакция откатилась, помечаем ошибки в слайсе
				for _, i := range items {
					batch[i].Idx = 0
					batch[i].Err = errInternalServerError
				}
			}
		}(items, s.shards[sID])
	}
	wg.Wait()

	return batch, nil
}

func (s *PgRepoDrv) Set(ctx context.Context, shardID byte, idx uint64, u string) error {
	shard, err := s.getShard(shardID)
	if err != nil {
		return fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	err = shard.PgPool(ctx, func(ctx context.Context, p *pgxpool.Pool) (err error) {
		_, err = p.Exec(ctx, sqlSet, idx, u)
		return err
	})

	if err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", sqlSet, err)
	}

	return nil
}

// По строке-идентификатору возвращает ранее записанную строку
func (s *PgRepoDrv) Select(ctx context.Context, shardID byte, idx uint64) (string, error) {
	shard, err := s.getShard(shardID)
	if err != nil {
		return "", fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	var u string
	err = shard.PgPool(ctx, func(ctx context.Context, p *pgxpool.Pool) error {
		return p.QueryRow(ctx, sqlSelect, idx).Scan(&u)
	})

	if err != nil {
		return "", fmt.Errorf("failed to execute query (%s): %w", sqlSelect, err)
	}

	return u, nil
}

func (s *PgRepoDrv) Ping(ctx context.Context) error {
	for _, shrd := range s.shards {
		if err := shrd.Ping(ctx); err != nil {
			return err
		}
	}

	return nil
}
