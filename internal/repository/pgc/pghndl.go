package pgc

import (
	"context"
	"errors"
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

var ErrBadPgConnString = errors.New("Bad connString for pool")
var ErrPanicRecovered = errors.New("Panic recovered")
var ErrPgHndlOffline = errors.New("PgHndl offline")

type IPgxPool interface {
	Ping(ctx context.Context) error
	Begin(ctx context.Context) (pgx.Tx, error)
	Close()
	Config() *pgxpool.Config
}

type PgHndl struct {
	mu              sync.RWMutex
	name            string
	pgPool          IPgxPool
	isReady         atomic.Bool
	isClosed        atomic.Bool
	lastCheckTime   time.Time
	lastCheckResult error
	checkInProgress sync.Mutex
	lastLatency     time.Duration
}

func NewPgHndl(ctx context.Context, name string, connString string) (*PgHndl, error) {
	pool, err := pgxpool.New(ctx, connString)

	if err != nil {
		return nil, errors.Join(ErrBadPgConnString, err)
	}

	h := &PgHndl{
		name:            name,
		pgPool:          pool,
		lastCheckTime:   time.Now(),
		lastCheckResult: nil,
	}
	h.isClosed.Store(false)
	h.Online()

	return h, nil
}

func (h *PgHndl) Name() string {
	return h.name
}

func (h *PgHndl) Host() string {
	return h.pgPool.Config().ConnConfig.Host
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

func (h *PgHndl) Ping(ctx context.Context) error {
	tm := time.Now()

	// Быстрая проверка
	h.mu.RLock()
	if tm.Sub(h.lastCheckTime) < 3*time.Second {
		res := h.lastCheckResult
		h.mu.RUnlock()
		return res
	}
	h.mu.RUnlock()

	// Если не получается взять лок
	if !h.checkInProgress.TryLock() {
		h.mu.RLock()
		res := h.lastCheckResult
		h.mu.RUnlock()
		return res
	}

	defer h.checkInProgress.Unlock()

	pCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	// Продолжительный Ping
	start := time.Now()
	err := h.pgPool.Ping(pCtx)
	duration := time.Since(start)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.lastCheckResult = err
	h.lastCheckTime = time.Now()
	h.lastLatency = duration

	if h.lastCheckResult == nil {
		h.Online()
	}
	if IsNetworkError(h.lastCheckResult) {
		h.Offline()
	}
	return h.lastCheckResult
}

func (h *PgHndl) Latency() time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastLatency
}

func (h *PgHndl) Begin(ctx context.Context, cb func(pgx.Tx) error) (err error) {
	if !h.IsReady() {
		if errPing := h.Ping(ctx); errPing != nil {
			return errors.Join(ErrPgHndlOffline, errPing)
		}
	}

	tx, err := h.pgPool.Begin(ctx)
	if err != nil {
		if IsNetworkError(err) {
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
				err = ErrPanicRecovered
			} else {
				err = errors.Join(ErrPanicRecovered, err)
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
