package pgc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgHndl коннектор, предохраняющий БД от дополнительной нагрузки, когда БД "плохо"
// и предохраняющий приложение от каскадного сбоя при проблемах с БД
type PgHndl struct {
	checkInProgress sync.Mutex
	name            string
	host            string
	pgPool          PgxPoolIface
	isReady         atomic.Bool
	isClosed        atomic.Bool
	lastCheckTime   atomic.Int64
	lastCheckResult atomic.Value
	lastLatency     atomic.Int64
}

// PgxPoolIface описывает методы pgxpool.Pool.
// Используем интерфейс, чтобы можно было в тестах замокать БД
type PgxPoolIface interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Ping(ctx context.Context) error
	Close()
	Config() *pgxpool.Config
}

//go:generate mockgen -destination=pgc_mock_test.go -package=pgc . PgxPoolIface

type checkResult struct {
	err error
}

func NewPgHndl(ctx context.Context, name string, connString string) (*PgHndl, error) {
	slog.Info("connString", slog.String("connString", connString))

	pool, err := pgxpool.New(ctx, connString)

	if err != nil {
		return nil, fmt.Errorf("bad connString (%s) for pool (connString): %w", connString, err)
	}

	h := &PgHndl{
		name:   name,
		pgPool: pool,
		host:   pool.Config().ConnConfig.Host,
	}

	h.lastCheckResult.Store(checkResult{err: nil})
	h.isClosed.Store(false)

	h.Ping(ctx)

	return h, nil
}

func (h *PgHndl) Name() string {
	return h.name
}

func (h *PgHndl) Host() string {
	return h.host
}

func (h *PgHndl) IsReady() bool {
	return h.isReady.Load()
}

func (h *PgHndl) Online() {
	if h.isClosed.Load() {
		slog.Error("The PgHndl closed",
			slog.String("name", h.Name()),
			slog.String("instance", h.Host()),
		)
		return
	}
	oldVal := h.isReady.Load()
	h.isReady.Store(true)

	if oldVal == false {
		slog.Info("The PgHndl has gone Online",
			slog.String("name", h.Name()),
			slog.String("instance", h.Host()),
		)
	}
}

func (h *PgHndl) Offline() {
	oldVal := h.isReady.Load()
	h.isReady.Store(false)

	if oldVal == true {
		slog.Info("The PgHndl has gone Offline",
			slog.String("name", h.Name()),
			slog.String("instance", h.Host()),
		)
	}
}

func (h *PgHndl) Close() {
	h.isClosed.Store(true)
	h.Offline()
	h.pgPool.Close()
}

// Ping Проверяет работоспособность БД
func (h *PgHndl) Ping(ctx context.Context) error {
	// Быстрая проверка
	lastCheck := h.lastCheckTime.Load()
	now := time.Now().UnixNano()

	if now-lastCheck < int64(3*time.Second) {
		return h.lastCheckResult.Load().(checkResult).err
	}

	// Если не получается взять лок
	if !h.checkInProgress.TryLock() {
		return h.lastCheckResult.Load().(checkResult).err
	}

	defer h.checkInProgress.Unlock()

	pCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	// Продолжительный Ping
	start := time.Now()
	err := h.pgPool.Ping(pCtx)
	duration := time.Since(start)

	now = time.Now().UnixNano()
	h.lastCheckTime.Store(now)
	h.lastCheckResult.Store(checkResult{err: err})
	h.lastLatency.Store(int64(duration))

	// Если ошибок нет, то переводим коннект в онлайн
	if err == nil {
		h.Online()
	}

	// Если произошла сетевая ошибка или мы не дождались Ping'а, то переводим коннект в Offline
	if IsNetworkError(err) || errors.Is(err, context.DeadlineExceeded) {
		h.Offline()
	}

	return err
}

func (h *PgHndl) Latency() time.Duration {
	return time.Duration(h.lastLatency.Load())
}

func (h *PgHndl) Begin(ctx context.Context, cb func(pgx.Tx) error) (err error) {
	if !h.IsReady() {
		if errPing := h.Ping(ctx); errPing != nil {
			return fmt.Errorf("database connection is offline: %w", errPing)
		}
	}

	tx, err := h.pgPool.Begin(ctx)
	if err != nil {
		if IsNetworkError(err) || errors.Is(err, context.DeadlineExceeded) {
			h.Offline()
		}
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error(
				"PANIC recovered",
				slog.Any("r", r),
				slog.String("stack", string(debug.Stack())),
			)
			if err == nil {
				err = fmt.Errorf("panic recovered")
			} else {
				err = fmt.Errorf("panic recovered: %w", err)
			}
		}
	}()

	defer tx.Rollback(context.WithoutCancel(ctx))

	// Вызов коллбека
	err = cb(tx)
	if err != nil {
		if IsNetworkError(err) {
			h.Offline()
		}
		return err // Тут сработает defer tx.Rollback()
	}

	err = tx.Commit(context.WithoutCancel(ctx))
	if err != nil {
		if IsNetworkError(err) {
			h.Offline()
		}
		return err
	}

	return nil
}

func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code[:2] == "08" {
		return true
	}
	return false
}
