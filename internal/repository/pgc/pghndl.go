package pgc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/migrations"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/cpu"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	goose "github.com/pressly/goose/v3"
)

//go:generate mockgen -source=$GOFILE -destination=pghndl_mocks_test.go -package=$GOPACKAGE
//go:generate mockgen -destination=pghndl_pgx_mocks_test.go -package=$GOPACKAGE github.com/jackc/pgx/v5 Tx,Row

// Чтобы написать UNIT тесты вводим интерфейс.
// pgPoolProvider описывает методы pgxpool.Pool, используемые модулем PgHndl
type pgPoolProvider interface {
	Begin(context.Context) (pgx.Tx, error)
	Ping(context.Context) error
	Config() *pgxpool.Config
	Close()
}

type PgPool interface {
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Ping(ctx context.Context) error
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	Stat() *pgxpool.Stat
}

// PgHndl коннектор, предохраняющий БД от дополнительной нагрузки, когда БД "плохо"
// и предохраняющий приложение от каскадного сбоя при проблемах с БД
type PgHndl struct {
	mu              sync.Mutex
	_               cpu.CacheLinePad // Выравнивание на случай, когда нужно работать с массивом PgHndls
	name            string
	instance        string
	pgPoolProv      pgPoolProvider // PgHndl управляет pgx пулом через интерфейс
	pgPool          *pgxpool.Pool  // А в колбек передается оригинальный объект
	isReady         atomic.Bool
	isClosed        atomic.Bool
	lastCheckResult atomic.Value

	failures  *FailureCounter
	repeater  *pgBackoff
	lastRetry atomic.Int64
}

func NewPgHndl(ctx context.Context, name string, connString string) (*PgHndl, error) {
	pool, err := pgxpool.New(ctx, connString)

	if err != nil {
		return nil, fmt.Errorf("bad connString for pool: %w", err)
	}

	connCfg := pool.Config().ConnConfig

	h := &PgHndl{
		name:       name,
		pgPoolProv: pool,
		pgPool:     pool,
		instance:   fmt.Sprintf("%s:%d/%s", connCfg.Host, connCfg.Port, connCfg.Database),

		failures: NewFailureCounter(3, 2*time.Second), // подряд 3 ошибки за 2 секунды и надо перевести PgHndl в Offline
		repeater: NewPgBackoff(3, 3*time.Second),      // 3 запроса к БД в течение 3 сек
	}

	h.isClosed.Store(false)
	h.isReady.Store(false)

	h.Ping(ctx)

	return h, nil
}

func (h *PgHndl) RunMigrations(ctx context.Context) error {
	goose.SetBaseFS(migrations.MigrationsDir)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	migrationsPath := "."

	slog.Info("Running migrations",
		slog.String("name", h.Name()),
		slog.String("db", h.Instance()),
		slog.String("migrations path", migrationsPath),
	)

	// Не закрываем БД из пула
	db := stdlib.OpenDBFromPool(h.pgPool)

	if err := goose.UpContext(ctx, db, migrationsPath); err != nil {
		return fmt.Errorf("failed to migrate PgHndl %s: %w", h.Instance(), err)
	}

	slog.Info("Successfully migrated database",
		slog.String("name", h.Name()),
		slog.String("db", h.Instance()),
	)

	return nil
}

// Name имя хэндла, заданное при инициализации
func (h *PgHndl) Name() string {
	return h.name
}

// Instance hostname:port/database соединения к БД
func (h *PgHndl) Instance() string {
	return h.instance
}

// IsReady DB готова к работе, если нет проблем с ошибками и коннект не закрыт
func (h *PgHndl) IsReady() bool {
	return !h.isClosed.Load() && h.isReady.Load()
}

// Online перевод хэндла в IsReady режим
func (h *PgHndl) Online() {
	if h.isReady.CompareAndSwap(false, true) {
		slog.Info("The PgHndl has gone Online", slog.String("name", h.name), slog.String("instance", h.instance))
	}
}

// Offline перевод хэндла в !IsReady режим
func (h *PgHndl) Offline() {
	if h.isReady.CompareAndSwap(true, false) {
		slog.Info("The PgHndl has gone Offline", slog.String("name", h.name), slog.String("instance", h.instance))
	}
}

// Close перевод хэндла в IsClosed && !IsReady режим
func (h *PgHndl) Close() {
	if h.isClosed.CompareAndSwap(false, true) {
		h.pgPoolProv.Close()
		slog.Info("The PgHndl closed",
			slog.String("name", h.Name()),
			slog.String("instance", h.Instance()),
		)
		h.Offline()
	}
}

func (h *PgHndl) HandleError(err error) error {
	if err == nil {
		h.failures.Reset()
		h.Online()
		return nil
	}
	if h.failures.Inc(err) {
		h.Offline()
	}

	return err
}

func (h *PgHndl) CanTry() bool {
	if h.IsReady() {
		return true
	}

	// Если мы Offline, проверяем, прошло ли, например, 5 секунд с последней попытки
	now := time.Now().Unix()
	last := h.lastRetry.Load()

	if now-last >= 5 { // Интервал пробы
		return h.lastRetry.CompareAndSwap(last, now)
	}
	return false
}

// Ping Проверяет работоспособность БД.
// По результатам работы устанавливается флаг isReady и сбрасывается счетчик h.failures
func (h *PgHndl) Ping(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	pCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 1*time.Second)
	defer cancel()

	return h.HandleError(h.pgPoolProv.Ping(pCtx))
}

func (h *PgHndl) Tx(ctx context.Context, cb func(ctx context.Context, tx pgx.Tx) error) error {
	return h.repeater.WithRetry(ctx, func() (err error) {
		var tx pgx.Tx

		defer func() {
			if r := recover(); r != nil {
				slog.Error(
					"PANIC recovered",
					slog.Any("r", r),
					slog.Any("err", err),
					slog.String("stack", string(debug.Stack())),
				)
				if err != nil {
					err = fmt.Errorf("panic recovered: %w", err)
				} else {
					err = errors.New("panic recovered")
				}
			}

			if err != nil && tx != nil {
				_ = tx.Rollback(context.WithoutCancel(ctx))
			}
		}()

		if !h.CanTry() {
			return errors.New("PgHndl is not ready")
		}

		tx, err = h.pgPoolProv.Begin(ctx)
		if err != nil {
			return h.HandleError(err)
		}

		err = cb(ctx, tx)
		if err != nil {
			return h.HandleError(err)
		}

		err = tx.Commit(context.WithoutCancel(ctx))
		if errors.Is(err, pgx.ErrTxClosed) {
			err = nil
		}

		return h.HandleError(err)
	})
}

func (h *PgHndl) PgPool(ctx context.Context, cb func(ctx context.Context, pool PgPool) error) error {
	return h.repeater.WithRetry(ctx, func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error(
					"PANIC recovered",
					slog.Any("r", r),
					slog.Any("err", err),
					slog.String("stack", string(debug.Stack())),
				)
				if err != nil {
					err = fmt.Errorf("panic recovered: %w", err)
				} else {
					err = errors.New("panic recovered")
				}
			}
		}()

		if !h.CanTry() {
			return errors.New("PgHndl is not ready")
		}

		err = cb(ctx, h.pgPool)
		return h.HandleError(err)
	})
}
