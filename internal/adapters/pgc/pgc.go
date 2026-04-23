// Package pgc предоставляет типизированную обертку над драйвером PostgreSQL,
// поддерживающую итераторы Go 1.23, телеметрию и автоматические проверки типов.
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

// ... импорты ...

// PgQuery описывает нетипизированный манифест SQL запроса.
// Обычно реализуется автоматически сгенерированными структурами.
type PgQuery interface {
	// SQL возвращает строку запроса с плейсхолдерами ($1, $2...).
	SQL() string
	// Name возвращает уникальное имя запроса для логирования и метрик.
	Name() string
	// IsReadOnly возвращает true, если запрос не изменяет данные.
	IsReadOnly() bool
	// HasReturns возвращает true, если запрос подразумевает возврат строк.
	HasReturns() bool
	// Binder связывает поля структуры с аргументами запроса.
	Binder(target any) []any
	// NewTarget создает новый экземпляр структуры для сканирования результата.
	NewTarget() any // Создает новый экземпляр структуры T
}

// PgInstance определяет интерфейс исполнителя запросов.
// Позволяет абстрагировать бизнес-логику от конкретной реализации (pool, conn, transaction).
type PgInstance interface {
	// Fetch выполняет запрос и возвращает итератор для ленивого чтения строк.
	Fetch(ctx context.Context, q PgQuery, args ...any) iter.Seq2[any, error]
	// FetchRow выполняет запрос и возвращает одну строку. Если строк нет, возвращает ошибку.
	FetchRow(ctx context.Context, q PgQuery, args ...any) (any, error)
	// Exec выполняет команду (INSERT/UPDATE/DELETE) и возвращает количество затронутых строк.
	Exec(ctx context.Context, q PgQuery, args ...any) (int64, error)
	// SendBatch выполняет пакет однотипных запросов в одной транзакции/пакете.
	SendBatch(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error]
	// IsOnline проверяет, доступен ли инстанс для выполнения запросов.
	IsOnline() bool
	// RunMigrations запускает встроенные миграции для текущего инстанса.
	RunMigrations(ctx context.Context) error
	// String возвращает строковое представление подключения (host:port/db).
	String() string
	// Ping проверяет физическое соединение с базой данных.
	Ping(ctx context.Context) error
	// Close корректно завершает работу инстанса.
	Close(ctx context.Context) error

	// Настройки телеметрии
	WithTracerProvider(trace.TracerProvider) PgInstance
	WithMeterProvider(metric.MeterProvider) PgInstance
	WithSlogHandler(slog.Handler) PgInstance
}

// Fetch — это типизированная обертка над PgInstance.Fetch.
// Использует итераторы Go 1.23 для удобного обхода результатов в цикле for-range.
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

// FetchRow — это типизированная обертка над PgInstance.FetchRow.
// Возвращает ровно один экземпляр типа T или ошибку.
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

// FetchRow — это типизированная обертка над PgInstance.Exec.
// Возвращает количество измененных строк или ошибку.
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
