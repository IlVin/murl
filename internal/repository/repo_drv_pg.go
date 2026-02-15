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

// =============================================
//
//	PgDrv
//
// =============================================

//go:generate mockgen -source=$GOFILE -destination=repo_drv_pg_mocks_test.go -package=$GOPACKAGE

// Чтобы протестировать драйвер постгреса, приходится городить интерфейс
// DBHandler описывает методы pgc.PgHndl для мокирования в тестах
type DBHandler interface {
	Tx(ctx context.Context, cb func(ctx context.Context, tx pgx.Tx) error) error
	PgPool(ctx context.Context, cb func(ctx context.Context, p pgc.PgPool) error) error
	Ping(ctx context.Context) error
	Name() string
	Instance() string
	RunMigrations(context.Context) error
	Close() error
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

	pgHndl.RunMigrations(ctx)

	for i := 0; i < shardSize; i++ {
		r.shards[i] = pgHndl
	}

	slog.Info("Use PgRepoDrv")

	return r, nil
}

func (s *PgRepoDrv) Close() error {
	errs := make([]error, 0, 10)
	for i := range s.shards {
		errs = append(errs, s.shards[i].Close())
	}
	return errors.Join(errs...)
}

func (s *PgRepoDrv) RunMigrations(ctx context.Context) error {
	// Собираем уникальные инстансы
	instances := make(map[string]DBHandler)
	for i, shard := range s.shards {
		instances[shard.Instance()+"/"+shard.Name()] = s.shards[i]
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

// UpSert Записывает строку в указанный шард БД и возвращает идентификатор записи и флаг конфликта
func (s *PgRepoDrv) UpSert(ctx context.Context, shardID byte, str string) (uint64, bool, error) {
	shard, err := s.getShard(shardID)
	if err != nil {
		return 0, false, err
	}

	var id uint64
	var cf bool

	errTx := shard.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlUpSert, str).Scan(&id, &cf)
	})
	if errTx != nil {
		return 0, false, fmt.Errorf("failed to execute query (%s): %w", sqlUpSert, errTx)
	}

	return id, cf, nil
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
type shardBatch struct {
	batch *pgx.Batch
	items []event.PayloadBatchItem
}

func (s *PgRepoDrv) BatchUpSert(ctx context.Context, batch []event.PayloadBatchItem) ([]event.PayloadBatchItem, error) {
	// Группируем элементы батча по шардам
	byShardBatch := make(map[byte]*shardBatch)
	for i := range batch {
		sID := batch[i].ShardID
		if byShardBatch[sID] == nil {
			byShardBatch[sID] = &shardBatch{
				batch: &pgx.Batch{},
				items: make([]event.PayloadBatchItem, 0, len(batch)/len(s.shards)+1),
			}
		}
		byShardBatch[sID].items = append(byShardBatch[sID].items, batch[i])
		byShardBatch[sID].batch.Queue(sqlUpSert, batch[i].OrigURL)
	}

	// Запускаем горутины
	resCh := make(chan event.PayloadBatchItem, len(batch))
	wg := sync.WaitGroup{}
	for sID, b := range byShardBatch {
		wg.Add(1)
		go func(sID byte, b *shardBatch) {
			defer wg.Done()

			shard, err := s.getShard(sID)
			if err != nil {
				// Весь батч ошибочный
				slog.Error("shard access failed",
					slog.Any("err", err),
					slog.Int("sID", int(sID)),
				)
				for i := range b.items {
					b.items[i].Idx = 0
					b.items[i].ConflictFlag = false
					b.items[i].Err = errInternalServerError
					resCh <- b.items[i]
				}
				return
			}

			errPool := shard.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				br := tx.SendBatch(ctx, b.batch)
				defer br.Close()

				// Вычитываем все результаты
				for i := range b.items {
					if err := ctx.Err(); err != nil {
						return err
					}
					if err := br.QueryRow().Scan(&b.items[i].Idx, &b.items[i].ConflictFlag); err != nil {
						return err
					}
				}

				return nil
			})

			// Весь батч ошибочный
			if errPool != nil {
				slog.Error("shard batch failed",
					slog.Any("err", errPool),
					slog.Int("sID", int(sID)),
				)
				for i := range b.items {
					b.items[i].Idx = 0
					b.items[i].ConflictFlag = false
					b.items[i].Err = errInternalServerError
				}
			}

			// Пишем результат в канал
			for i := range b.items {
				resCh <- b.items[i]
			}

		}(sID, b)
	}

	// Ждем джобы и закрываем результирующий канал
	go func() {
		wg.Wait()
		close(resCh)
	}()

	result := make([]event.PayloadBatchItem, 0, len(batch))
	for range len(batch) {
		select {
		case res, ok := <-resCh:
			if !ok {
				return result, nil
			}
			result = append(result, res)
		}
	}
	return result, nil
}

func (s *PgRepoDrv) Set(ctx context.Context, shardID byte, idx uint64, u string) error {
	shard, err := s.getShard(shardID)
	if err != nil {
		return fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	err = shard.PgPool(ctx, func(ctx context.Context, p pgc.PgPool) error {
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
	err = shard.PgPool(ctx, func(ctx context.Context, p pgc.PgPool) error {
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
