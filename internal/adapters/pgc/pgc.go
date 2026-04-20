package pgc

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net"
	"runtime/debug"
	"strings"

	pgconn "github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE         -destination=pgc_mock_test.go  -package=$GOPACKAGE

// --- Интерфейсы ---

// PgQuery описывает нетипизированный манифест SQL запроса
type PgQuery interface {
	SQL() string
	Name() string
	IsReadOnly() bool
	HasReturns() bool
	Binder(target any) []any
	NewTarget() any // Создает новый экземпляр структуры T
}

// PgInstance — интерфейс любого исполнителя SQL запроса.
type PgInstance interface {

	// Fetch выполняет запрос и возвращает итератор
	Fetch(ctx context.Context, q PgQuery, args ...any) iter.Seq2[any, error]

	// FetchRow выполняет запрос на одну строку
	FetchRow(ctx context.Context, q PgQuery, args ...any) (any, error)

	// Exec выполняет команду (INSERT/UPDATE/DELETE)
	Exec(ctx context.Context, q PgQuery, args ...any) (int64, error)

	// SendBatch выполняет пакет однотипных запросов.
	// Возвращает плоский поток результатов (один за другим).
	SendBatch(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error]

	// IsOnline признак того, что соединение с БД не отключено предохранителем
	IsOnline() bool

	// RunMigrations запуск миграций на инстансе
	RunMigrations(ctx context.Context) error

	// String стрингер "hostname:port/database"
	String() string

	// Ping Проверяет работоспособность БД.
	Ping(ctx context.Context) error

	// Close перевод хэндла в IsClosed && !IsReady режим
	Close(ctx context.Context) error

	// Кастомные настройки провайдеров Tracer, Meter, Logger
	WithTracerProvider(trace.TracerProvider) PgInstance
	WithMeterProvider(metric.MeterProvider) PgInstance
	WithSlogHandler(slog.Handler) PgInstance
}

// --- Извлечение данных ---

// Fetch извлекает поток данных в стиле Go 1.23 Iterators.
func Fetch[T any](ctx context.Context, pg PgInstance, q *Query[T], args ...any) iter.Seq2[T, error] {
	rawSeq := pg.Fetch(ctx, q, args...)
	return func(yield func(T, error) bool) {
		for val, err := range rawSeq {
			if err != nil {
				var zero T
				if !yield(zero, err) {
					return
				}
				return // При ошибке БД итерация должна прекратиться
			}

			// val — это *T, возвращенный из драйвера
			ptr, ok := val.(*T)
			if !ok {
				var zero T
				if !yield(zero, fmt.Errorf("pgc: driver returned invalid type %T, expected *%T", val, zero)) {
					return
				}
				continue
			}

			if !yield(*ptr, nil) {
				return
			}
		}
	}
}

// FetchRow извлекает ровно одну строку.
func FetchRow[T any](ctx context.Context, pg PgInstance, q *Query[T], args ...any) (T, error) {
	var zero T
	res, err := pg.FetchRow(ctx, q, args...)
	if err != nil {
		return zero, err
	}

	ptr, ok := res.(*T)
	if !ok {
		return zero, fmt.Errorf("pgc: driver returned invalid type %T, expected *%T", res, zero)
	}
	return *ptr, nil
}

// Exec выполняет команду (INSERT/UPDATE/DELETE).
func Exec(ctx context.Context, pg PgInstance, q PgQuery, args ...any) (int64, error) {
	return pg.Exec(ctx, q, args...)
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// 1. Проверяем стандартный интерфейс net.Error (Timeout или Temporary)
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true // Любая сетевая ошибка для нас — повод для ретрая в CQRS
	}

	// 2. Проверяем конкретные коды Postgres
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "57P01", "57P02", "57P03", "53300":
			return true
		}
	}

	return false
}

func getInstrumentationName(subsystem string) string {
	name := []string{"pgc"} // Дефолт

	if buildInfo, ok := debug.ReadBuildInfo(); ok && buildInfo.Main.Path != "" {
		parts := strings.Split(buildInfo.Main.Path, "/")
		name = append([]string{parts[len(parts)-1]}, name...)
	}

	if subsystem != "" {
		name = append(name, subsystem)
	}

	return strings.Join(name, ".")
}
