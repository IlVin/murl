package pgc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/migrations"
	"net"
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

// ========= [ Какую проблему решает этот модуль ] ===========
// Модуль реализует паттерн Circuit Breaker (Предохранитель)
//
// В распределенных системах одна из самых опасных проблем — это каскадный сбой.
// Если база данных (БД) начинает тормозить или временно становится недоступной,
// приложение может «захлебнуться», бесконечно пытаясь установить новые соединения,
// занимая потоки выполнения и память в ожидании тайм-аутов. Это приводит к тому,
// что ложится и сервис, и база.
// Модуль PgHndl решает следующие задачи:
// + Защита БД от перегрузки (Fail-Fast): Если БД «плохо», коннектор переходит в состояние Offline.
// + Защита приложения от зависания: Приложение не ждет стандартных долгих тайм-аутов TCP/Postgres, а анализирует флаг IsReady
// + Автоматическое восстановление (Self-Healing): Через метод Ping и логику в HandleDBError коннектор периодически проверяет состояние базы.
// + Безопасное выполнение (Panic Recovery): Методы Tx (транзакция) и PgPool (прямой доступ) оборачивают вызовы пользовательских функций в recover().

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

// PgHndl коннектор, предохраняющий БД от дополнительной нагрузки, когда БД "плохо"
// и предохраняющий приложение от каскадного сбоя при проблемах с БД
type PgHndl struct {
	mu              sync.Mutex
	_               cpu.CacheLinePad // Выравнивание на случай, когда нужно работать с массивом PgHndls
	name            string
	host            string
	pgPoolProv      pgPoolProvider // PgHndl управляет pgx пулом через интерфейс
	pgPool          *pgxpool.Pool  // А в колбек передается оригинальный объект
	isReady         atomic.Bool
	isClosed        atomic.Bool
	lastCheckResult atomic.Value
}

type checkResult struct {
	err       error
	timestamp time.Time
	latency   time.Duration
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
		host:       fmt.Sprintf("%s:%d/%s", connCfg.Host, connCfg.Port, connCfg.Database),
	}

	h.lastCheckResult.Store(checkResult{
		timestamp: time.Now().Add(-5 * time.Second),
		latency:   0,
		err:       nil,
	})
	h.isClosed.Store(false)
	h.isReady.Store(false)

	h.Ping(ctx)

	return h, nil
}

func (h *PgHndl) RunMigrations(ctx context.Context) error {
	goose.SetBaseFS(migrations.MigrationsDir)

	// Устанавливаем диалект базы данных для goose
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	migrationsPath := "."

	slog.Info("Running migrations",
		slog.String("name", h.Name()),
		slog.String("db", h.Host()),
		slog.String("migrations path", migrationsPath),
	)

	// Превращаем *pgxpool.Pool в *sql.DB без создания нового физического пула.
	db := stdlib.OpenDB(*h.pgPoolProv.Config().ConnConfig)
	defer db.Close()

	// Выполняем миграции
	if err := goose.UpContext(ctx, db, migrationsPath); err != nil {
		return fmt.Errorf("failed to migrate PgHndl %s: %w", h.Host(), err)
	}

	slog.Info("Successfully migrated database",
		slog.String("name", h.Name()),
		slog.String("db", h.Host()),
	)

	return nil
}

// HandleDBError Логика, переводящая хэндл в онлайн или оффлайн
func (h *PgHndl) HandleDBError(err error) error {
	if err != nil && (IsNetworkError(err) || errors.Is(err, context.DeadlineExceeded)) {
		h.Offline()
	} else if !h.IsReady() {
		h.Online()
	}
	return err
}

// Name имя хэндла, заданное при инициализации
func (h *PgHndl) Name() string {
	return h.name
}

// Host hostname:port соединения к БД
func (h *PgHndl) Host() string {
	return h.host
}

// Latency в наносекундах
func (h *PgHndl) Latency() time.Duration {
	if res, ok := h.lastCheckResult.Load().(checkResult); ok {
		return res.latency
	}
	return 0
}

// IsReady DB готова к работе, если нет проблем с ошибками и коннект не закрыт
func (h *PgHndl) IsReady() bool {
	return h.isReady.Load() && !h.isClosed.Load()
}

// Online перевод хэндла в IsReady режим
func (h *PgHndl) Online() {
	if h.isClosed.Load() {
		return
	}
	if h.isReady.CompareAndSwap(false, true) {
		slog.Info("The PgHndl has gone Online", slog.String("name", h.name), slog.String("instance", h.host))
	}
}

// Offline перевод хэндла в !IsReady режим
func (h *PgHndl) Offline() {
	if h.isReady.CompareAndSwap(true, false) {
		slog.Info("The PgHndl has gone Offline", slog.String("name", h.name), slog.String("instance", h.host))
	}
}

// Close перевод хэндла в IsClosed && !IsReady режим
func (h *PgHndl) Close() {
	h.Offline()
	h.isClosed.Store(true)
	h.pgPoolProv.Close()
	slog.Info("The PgHndl closed",
		slog.String("name", h.Name()),
		slog.String("instance", h.Host()),
	)
}

// Ping Проверяет работоспособность БД
func (h *PgHndl) Ping(ctx context.Context) error {
	// Защита от nil контекста, чтобы WithoutCancel не паниковал
	if ctx == nil {
		ctx = context.Background()
	}

	// Быстрая проверка
	if res, ok := h.lastCheckResult.Load().(checkResult); ok {
		if time.Since(res.timestamp) < 3*time.Second {
			return res.err
		}
	}

	// Если не получается взять лок
	if !h.mu.TryLock() {
		if res, ok := h.lastCheckResult.Load().(checkResult); ok {
			return res.err
		}
		return nil
	}

	defer h.mu.Unlock()

	pCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 1*time.Second)
	defer cancel()

	// Продолжительный Ping
	start := time.Now()
	err := h.pgPoolProv.Ping(pCtx)

	// Сохранение результатов
	res := checkResult{
		timestamp: start,
		latency:   time.Since(start),
		err:       err,
	}
	h.lastCheckResult.Store(res)

	return h.HandleDBError(err)
}

func (h *PgHndl) Tx(ctx context.Context, cb func(ctx context.Context, tx pgx.Tx) (any, error)) (response any, err error) {
	if !h.IsReady() {
		if errPing := h.Ping(ctx); errPing != nil {
			return nil, fmt.Errorf("database connection is offline: %w", errPing)
		}
	}

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

		if err != nil {
			err = h.HandleDBError(err)
			if tx != nil {
				_ = tx.Rollback(context.WithoutCancel(ctx))
			}
		}
	}()

	tx, err = h.pgPoolProv.Begin(ctx)
	if err != nil {
		return // HandleDBError сработает в defer и обработает именованный err
	}

	// Вызов коллбека
	if response, err = cb(ctx, tx); err != nil {
		return // HandleDBError сработает в defer и обработает именованный err
	}

	err = tx.Commit(context.WithoutCancel(ctx))
	if errors.Is(err, pgx.ErrTxClosed) {
		err = nil // Считаем, что всё ок, транзакция уже завершена
	}
	return // HandleDBError сработает в defer и обработает именованный err
}

func (h *PgHndl) PgPool(ctx context.Context, cb func(ctx context.Context, pool *pgxpool.Pool) error) (err error) {
	if !h.IsReady() {
		if errPing := h.Ping(ctx); errPing != nil {
			return fmt.Errorf("database connection is offline: %w", errPing)
		}
	}

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
		err = h.HandleDBError(err)
	}()

	// Вызов коллбека
	err = cb(ctx, h.pgPool)

	return // HandleDBError сработает в defer и обработает именованный err
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
