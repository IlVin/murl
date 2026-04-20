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

// PgxPoolIface — только то, что нужно для работы с пулом
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

func (p *pgConnector) initMetrics() {
	p.mLatency, _ = p.meter.Float64Histogram("db.pgc.operation.duration",
		metric.WithUnit("s"))
}

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

// --- Реализация PgInstance ---

func (p *pgConnector) WithTracerProvider(tp trace.TracerProvider) PgInstance {
	if tp != nil {
		p.tracer = tp.Tracer(getInstrumentationName(otelNameConn))
	}
	return p
}

func (p *pgConnector) WithMeterProvider(mp metric.MeterProvider) PgInstance {
	if mp != nil {
		p.meter = mp.Meter(getInstrumentationName(otelNameConn))
		p.initMetrics()
	}
	return p
}

func (p *pgConnector) WithSlogHandler(h slog.Handler) PgInstance {
	if h != nil {
		p.logger = slog.New(h)
	}
	return p
}

func (p *pgConnector) IsOnline() bool {
	if p.isOnline.Load() > 0 {
		return true
	}
	return false
}

func (p *pgConnector) Fetch(ctx context.Context, q PgQuery, args ...any) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		// Мы используем execInternal, но результат обработки строк передаем через yield.
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
				// Мы выходим из анонимной функции, defer rows.Close() срабатывает.
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

func (p *pgConnector) FetchRow(ctx context.Context, q PgQuery, args ...any) (any, error) {
	return p.execInternal(ctx, q, "fetch_row", func(ctx context.Context) (any, error) {
		target := q.NewTarget()
		err := p.pool.QueryRow(ctx, q.SQL(), args...).Scan(q.Binder(target)...)
		return target, err
	})
}

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
			defer br.Close()

			for i := 0; i < len(args); i++ {
				rows, err := br.Query()
				if err != nil {
					return nil, err
				}
				for rows.Next() {
					target := q.NewTarget()
					if err := rows.Scan(q.Binder(target)...); err != nil {
						rows.Close()
						return nil, err
					}
					if !yield(target, nil) {
						rows.Close()
						return nil, nil
					}
				}
				rows.Close()
			}
			return nil, nil
		})

		if err != nil {
			var zero any
			yield(zero, err)
		}
	}
}

// --- Внутренняя обертка (Трейсинг + CB + Метрики + Паники) ---

func (p *pgConnector) execInternal(ctx context.Context, q PgQuery, op string, fn func(ctx context.Context) (any, error)) (any, error) {
	return p.cb.Execute(func() (any, error) {
		ctx, span := p.tracer.Start(ctx, q.Name(), trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", op),
			attribute.String("db.query.text", q.SQL()),
			attribute.String("server.address", p.host),
			attribute.String("db.namespace", p.database),
		))
		defer span.End()

		defer func() {
			if r := recover(); r != nil {
				p.logger.ErrorContext(ctx, "panic in db op",
					slog.Any("error", r),
					slog.String("stack", string(debug.Stack())),
				)

				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, "panic")
				panic(r)
			}
		}()

		start := p.now()
		res, err := fn(ctx)

		p.mLatency.Record(ctx, p.now().Sub(start).Seconds(), metric.WithAttributes(
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

// --- Служебные методы ---

func (p *pgConnector) RunMigrations(ctx context.Context) error {
	realPool, ok := p.pool.(*pgxpool.Pool)
	if !ok {
		return fmt.Errorf("pgc: migrations require a real *pgxpool.Pool, got %T", p.pool)
	}
	db := stdlib.OpenDBFromPool(realPool)
	goose.SetBaseFS(migrations.MigrationsDir)
	goose.SetDialect("postgres")
	return goose.UpContext(ctx, db, ".")
}

func (p *pgConnector) String() string                 { return fmt.Sprintf("%s:%d/%s", p.host, p.port, p.database) }
func (p *pgConnector) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *pgConnector) Close(_ context.Context) error  { p.pool.Close(); return nil }
