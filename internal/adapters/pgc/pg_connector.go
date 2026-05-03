package pgc

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"murl/migrations"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	pgconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	goose "github.com/pressly/goose/v3"
	"github.com/sony/gobreaker"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricNoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	traceNoop "go.opentelemetry.io/otel/trace/noop"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=pg_connector_mock_test.go  -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen                 -destination=pgx_mock_test.go           -package=$GOPACKAGE github.com/jackc/pgx/v5 Tx,Row,BatchResults,Rows

// PgxPoolIface - интерфейс, описывающий только необходимые для работы методы пула соединений.
// Позволяет легко создавать моки для тестирования.
type PgxPoolIface interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	Ping(ctx context.Context) error
	Stat() *pgxpool.Stat
	Close()
}

const otelNameConn = "conn"

// pgConnector реализует базовый PgInstance для работы с PostgreSQL через pgxpool.
// Обеспечивает:
//   - Автоматический Circuit Breaker для защиты от каскадных отказов
//   - OpenTelemetry трассировку каждого запроса
//   - Метрики длительности операций
//   - Восстановление после паник
//   - Потоковое чтение через итераторы Go 1.23
type pgConnector struct {
	host     string
	port     uint16
	database string
	pool     PgxPoolIface
	cb       *gobreaker.CircuitBreaker

	// Провайдеры
	tracer trace.Tracer
	meter  metric.Meter
	logger *slog.Logger

	// Метрики
	mLatency metric.Float64Histogram
	isOnline atomic.Int64 // 1 - OK, 0 - Trip

	now func() time.Time
}

// NewPgConnector создаёт новый коннектор к PostgreSQL.
// Принимает строку подключения в формате PostgreSQL (например, "postgres://user:pass@localhost:5432/db").
// Возвращает PgInstance, готовую к использованию, или ошибку при неудачном подключении.
func NewPgConnector(ctx context.Context, connString string) (PgInstance, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	cfg := pool.Config().ConnConfig
	p := &pgConnector{
		host:     cfg.Host,
		port:     cfg.Port,
		database: cfg.Database,
		pool:     pool,
		tracer:   traceNoop.NewTracerProvider().Tracer(getInstrumentationName(otelNameConn)),
		meter:    metricNoop.NewMeterProvider().Meter(getInstrumentationName(otelNameConn)),
		logger:   slog.Default(),
		now:      time.Now,
	}
	p.isOnline.Store(1)

	p.initMetrics()
	p.initCB()

	return p, nil
}

// initMetrics инициализирует метрики OpenTelemetry.
func (p *pgConnector) initMetrics() {
	p.mLatency, _ = p.meter.Float64Histogram("db.pgc.operation.duration",
		metric.WithUnit("s"))
}

// initCB инициализирует Circuit Breaker для защиты от каскадных отказов.
// При трёх последовательных ошибках переходит в состояние Open на 5 секунд.
func (p *pgConnector) initCB() {
	p.cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        p.String(),
		MaxRequests: 1,
		Interval:    10 * time.Second,
		Timeout:     5 * time.Second,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 3 },
		OnStateChange: func(_ string, _, to gobreaker.State) {
			if to == gobreaker.StateOpen {
				p.isOnline.Store(0)
			} else {
				p.isOnline.Store(1)
			}
		},
	})
}

// WithTracerProvider устанавливает TracerProvider для телеметрии.
// Возвращает тот же экземпляр для цепочечных вызовов.
func (p *pgConnector) WithTracerProvider(tp trace.TracerProvider) PgInstance {
	if tp != nil {
		p.tracer = tp.Tracer(getInstrumentationName(otelNameConn))
	}
	return p
}

// WithMeterProvider устанавливает MeterProvider для метрик.
// Возвращает тот же экземпляр для цепочечных вызовов.
func (p *pgConnector) WithMeterProvider(mp metric.MeterProvider) PgInstance {
	if mp != nil {
		p.meter = mp.Meter(getInstrumentationName(otelNameConn))
		p.initMetrics()
	}
	return p
}

// WithSlogHandler устанавливает обработчик структурного логирования.
// Возвращает тот же экземпляр для цепочечных вызовов.
func (p *pgConnector) WithSlogHandler(h slog.Handler) PgInstance {
	if h != nil {
		p.logger = slog.New(h)
	}
	return p
}

// IsOnline возвращает true, если коннектор доступен для выполнения запросов.
// Учитывает состояние Circuit Breaker: если breaker разомкнут, возвращает false.
func (p *pgConnector) IsOnline() bool {
	return p.isOnline.Load() > 0
}

// Fetch выполняет потоковое чтение данных из БД.
// Возвращает итератор, выдающий строки по одной.
// Ресурсы (rows) гарантированно закрываются при любом завершении.
//
// Пример использования:
//
//	for val, err := range db.Fetch(ctx, query, args...) {
//	    if err != nil {
//	        return err
//	    }
//	    // обработка val
//	}
func (p *pgConnector) Fetch(ctx context.Context, q PgQuery, args ...any) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		// Используем execInternal, но результат обработки строк передаем через yield.
		// Если execInternal вернет ошибку (например, CB Open или ошибка коннекта),
		// мы поймаем её в err и отдадим в yield.
		_, err := p.execInternal(ctx, q, "fetch", func(pCtx context.Context) (any, error) {
			rows, err := p.pool.Query(pCtx, q.SQL(), args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			for rows.Next() {
				target := q.NewTarget()
				if err := rows.Scan(q.Binder(target)...); err != nil {
					return nil, err
				}
				// Если пользователь вызвал break в цикле, yield вернет false.
				// Выходим из анонимной функции, defer rows.Close() срабатывает.
				if !yield(target, nil) {
					return nil, nil
				}
			}
			return nil, rows.Err()
		})

		// Если ошибка произошла на уровне Circuit Breaker или до начала итерации
		if err != nil {
			var zero any
			yield(zero, err)
		}
	}
}

// FetchRow выполняет запрос и возвращает одну строку.
// Если строк нет, возвращает ошибку.
// Удобен для запросов, которые должны вернуть ровно одну запись.
//
// Пример использования:
//
//	user, err := db.FetchRow(ctx, query, id)
//	if err != nil {
//	    return err
//	}
func (p *pgConnector) FetchRow(ctx context.Context, q PgQuery, args ...any) (any, error) {
	return p.execInternal(ctx, q, "fetch_row", func(ctx context.Context) (any, error) {
		target := q.NewTarget()
		err := p.pool.QueryRow(ctx, q.SQL(), args...).Scan(q.Binder(target)...)
		return target, err
	})
}

// Exec выполняет команду изменения данных (INSERT/UPDATE/DELETE).
// Возвращает количество затронутых строк или ошибку.
func (p *pgConnector) Exec(ctx context.Context, q PgQuery, args ...any) (int64, error) {
	res, err := p.execInternal(ctx, q, "exec", func(ctx context.Context) (any, error) {
		tag, err := p.pool.Exec(ctx, q.SQL(), args...)
		if err != nil {
			return nil, err
		}
		return tag.RowsAffected(), nil
	})
	if err != nil {
		return 0, err
	}
	return res.(int64), nil
}

// SendBatch выполняет пакет однотипных запросов.
// Каждый запрос в пакете обрабатывается последовательно.
// Ресурсы (rows) гарантированно закрываются при любом завершении через анонимные функции.
// Возвращает итератор, выдающий результаты каждого запроса в порядке отправки.
//
// Важно: если один из запросов в пакете завершается ошибкой,
// последующие запросы также вернут ошибку (семантика PostgreSQL batch).
func (p *pgConnector) SendBatch(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		// Используем execInternal, который уже умеет в трейсинг и CB
		_, err := p.execInternal(ctx, q, "batch", func(pCtx context.Context) (any, error) {
			batch := &pgx.Batch{}
			for _, argSet := range args {
				batch.Queue(q.SQL(), argSet...)
			}

			// Добавляем атрибут размера пачки к текущему спану через текущий контекст
			if span := trace.SpanFromContext(pCtx); span.IsRecording() {
				span.SetAttributes(attribute.Int("db.pgc.batch_size", len(args)))
			}

			br := p.pool.SendBatch(pCtx, batch)
			defer func() {
				if err := br.Close(); err != nil {
					slog.Error("batcher close fail",
						slog.Any("err", err),
					)
				}
			}()

			for i := 0; i < len(args); i++ {
				// Анонимная функция для гарантированного закрытия rows
				err := func() error {
					rows, err := br.Query()
					if err != nil {
						return err
					}
					defer rows.Close()

					for rows.Next() {
						target := q.NewTarget()
						if err := rows.Scan(q.Binder(target)...); err != nil {
							return err
						}
						if !yield(target, nil) {
							// Пользователь прервал итерацию
							return nil
						}
					}
					return rows.Err()
				}()
				if err != nil {
					return nil, err
				}
			}
			return nil, nil
		})

		if err != nil {
			var zero any
			yield(zero, err)
		}
	}
}

// execInternal — внутренняя обёртка, добавляющая ко всем операциям:
//   - OpenTelemetry трассировку
//   - Circuit Breaker
//   - Метрики длительности
//   - Восстановление после паник
//   - Логирование ошибок
func (p *pgConnector) execInternal(ctx context.Context, q PgQuery, op string, fn func(ctx context.Context) (any, error)) (any, error) {
	return p.cb.Execute(func() (any, error) {
		tracerCtx, span := p.tracer.Start(ctx, q.Name(), trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", op),
			attribute.String("db.query.text", q.SQL()),
			attribute.String("server.address", p.host),
			attribute.String("db.namespace", p.database),
		))
		defer span.End()

		defer func() {
			if r := recover(); r != nil {
				p.logger.ErrorContext(tracerCtx, "panic in db op",
					slog.Any("error", r),
					slog.String("stack", string(debug.Stack())),
				)

				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, "panic")
				panic(r)
			}
		}()

		start := p.now()
		res, err := fn(tracerCtx)

		p.mLatency.Record(tracerCtx, p.now().Sub(start).Seconds(), metric.WithAttributes(
			attribute.String("server.address", p.host),
			attribute.String("db.namespace", p.database),
		))

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return res, err
	})
}

// RunMigrations запускает миграции схемы базы данных.
// Использует встроенную файловую систему с миграциями.
// Требует, чтобы pool был реальным *pgxpool.Pool (не моком).
func (p *pgConnector) RunMigrations(ctx context.Context) error {
	realPool, ok := p.pool.(*pgxpool.Pool)
	if !ok {
		return fmt.Errorf("pgc: migrations require a real *pgxpool.Pool, got %T", p.pool)
	}
	db := stdlib.OpenDBFromPool(realPool)
	goose.SetBaseFS(migrations.MigrationsDir)
	if err := goose.SetDialect("postgres"); err != nil {
		slog.Error("goose SetDialect fail",
			slog.Any("err", err),
		)
	}
	return goose.UpContext(ctx, db, ".")
}

// String возвращает строковое представление коннектора.
// Формат: "host:port/database"
func (p *pgConnector) String() string {
	return fmt.Sprintf("%s:%d/%s", p.host, p.port, p.database)
}

// Ping проверяет физическое соединение с базой данных.
// Возвращает ошибку, если соединение не установлено.
func (p *pgConnector) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Close закрывает пул соединений.
// После вызова Close коннектор нельзя использовать.
func (p *pgConnector) Close(_ context.Context) error {
	p.pool.Close()
	return nil
}
