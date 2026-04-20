package pgc

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"murl/internal/model"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	traceNoop "go.opentelemetry.io/otel/trace/noop"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=pg_cluster_mock_test.go  -package=$GOPACKAGE

const otelNameCluster = "cluster"
const otelNameShard = "shard"

// PgCluster управляет набором шардов с поддержкой иерархической телеметрии.
type PgCluster interface {
	// ShardID возвращает идентификатор шарда по ключу на основе размера кластера.
	ShardID(key any) byte
	// GetShard возвращает прокси-инстанс шарда с предустановленным Trace-контекстом.
	GetShard(shardID byte) (PgInstance, error)
	// RunMigrations запускает миграции на всех уникальных физических инстансах.
	RunMigrations(ctx context.Context) error
	// Ping проверяет доступность всех физических инстансов.
	Ping(ctx context.Context) error
	// Close корректно закрывает все уникальные инстансы.
	Close(ctx context.Context) error
	// Size возвращает количество логических шардов.
	Size() byte

	WithTracerProvider(tp trace.TracerProvider) PgCluster
	WithMeterProvider(mp metric.MeterProvider) PgCluster
	WithSlogHandler(h slog.Handler) PgCluster
}

type pgCluster struct {
	uniqueInstances []PgInstance
	shards          []PgInstance

	tracer trace.Tracer
}

// NewPgCluster создает кластер, оборачивая каждый инстанс в логический прокси-слой.
func NewPgCluster(ctx context.Context, shardInsts []PgInstance) (PgCluster, error) {
	if len(shardInsts) == 0 {
		return nil, errors.New("pgc: cluster must have at least one shard")
	}

	uniqueMap := make(map[string]PgInstance)
	shards := make([]PgInstance, len(shardInsts))

	for i, pg := range shardInsts {
		if pg == nil {
			return nil, fmt.Errorf("pgc: shard at index %d is nil", i)
		}
		uniqueMap[pg.String()] = pg

		// Оборачиваем в прокси для мечения трейсов номером шарда
		shards[i] = &pgShardInst{
			PgInstance: pg,
			shardID:    byte(i),
			tracer:     traceNoop.NewTracerProvider().Tracer(getInstrumentationName(otelNameShard)),
		}
	}

	uniqueInstances := make([]PgInstance, 0, len(uniqueMap))
	for _, inst := range uniqueMap {
		uniqueInstances = append(uniqueInstances, inst)
	}

	c := &pgCluster{
		uniqueInstances: uniqueInstances,
		shards:          shards,
		tracer:          traceNoop.NewTracerProvider().Tracer(getInstrumentationName(otelNameCluster)),
	}

	return c, nil
}

// --- Логический слой шарда (Proxy) ---

type pgShardInst struct {
	PgInstance
	shardID byte

	tracer trace.Tracer
}

func (s *pgShardInst) startSpan(ctx context.Context, q PgQuery, op string) (context.Context, trace.Span) {
	return s.tracer.Start(ctx, q.Name(),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			// Семантические конвенции OTel
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation.name", op),
			attribute.String("db.query.text", q.SQL()),
			// Кастомные атрибуты кластера
			attribute.Int("db.pgc.shard_id", int(s.shardID)),
			attribute.Bool("db.pgc.is_readonly", q.IsReadOnly()),
		))
}

func (s *pgShardInst) WithTracerProvider(tp trace.TracerProvider) PgInstance {
	if tp != nil {
		s.tracer = tp.Tracer(getInstrumentationName(otelNameShard))
	}
	s.PgInstance.WithTracerProvider(tp)
	return s
}

func (s *pgShardInst) WithMeterProvider(mp metric.MeterProvider) PgInstance {
	s.PgInstance.WithMeterProvider(mp)
	return s
}

func (s *pgShardInst) WithSlogHandler(h slog.Handler) PgInstance {
	s.PgInstance.WithSlogHandler(h)
	return s
}

func (s *pgShardInst) Fetch(ctx context.Context, q PgQuery, args ...any) iter.Seq2[any, error] {
	spanCtx, span := s.startSpan(ctx, q, "fetch")
	return func(yield func(any, error) bool) {
		defer span.End()
		for v, err := range s.PgInstance.Fetch(spanCtx, q, args...) {
			if !yield(v, err) {
				return
			}
		}
	}
}

func (s *pgShardInst) FetchRow(ctx context.Context, q PgQuery, args ...any) (any, error) {
	spanCtx, span := s.startSpan(ctx, q, "fetch_row")
	defer span.End()

	res, err := s.PgInstance.FetchRow(spanCtx, q, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return res, err
}

func (s *pgShardInst) Exec(ctx context.Context, q PgQuery, args ...any) (int64, error) {
	spanCtx, span := s.startSpan(ctx, q, "exec")
	defer span.End()

	rows, err := s.PgInstance.Exec(spanCtx, q, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetAttributes(attribute.Int64("db.response.rows_affected", rows))
	}
	return rows, err
}

func (s *pgShardInst) SendBatch(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
	spanCtx, span := s.startSpan(ctx, q, "batch")
	span.SetAttributes(attribute.Int("db.pgc.batch_size", len(args)))

	return func(yield func(any, error) bool) {
		defer span.End()
		for v, err := range s.PgInstance.SendBatch(spanCtx, q, args) {
			if !yield(v, err) {
				return
			}
		}
	}
}

func (s *pgShardInst) String() string {
	return fmt.Sprintf("<Shard:%d>{%s}", s.shardID, s.PgInstance.String())
}

// --- Реализация PgCluster ---

func (c *pgCluster) WithTracerProvider(tp trace.TracerProvider) PgCluster {
	if tp != nil {
		c.tracer = tp.Tracer(getInstrumentationName(otelNameCluster))
	}
	for _, shard := range c.shards {
		shard.WithTracerProvider(tp)
	}
	return c
}

func (c *pgCluster) WithMeterProvider(mp metric.MeterProvider) PgCluster {
	for _, inst := range c.uniqueInstances {
		inst.WithMeterProvider(mp)
	}
	return c
}

func (c *pgCluster) WithSlogHandler(h slog.Handler) PgCluster {
	for _, inst := range c.uniqueInstances {
		inst.WithSlogHandler(h)
	}
	return c
}

func (c *pgCluster) ShardID(key any) byte {
	return model.ShardID(key, c.Size())
}

func (c *pgCluster) GetShard(shardID byte) (PgInstance, error) {
	if int(shardID) >= len(c.shards) {
		return nil, fmt.Errorf("pgc: shardID %d is out of range [0..%d)", shardID, len(c.shards))
	}
	return c.shards[shardID], nil
}

func (c *pgCluster) RunMigrations(ctx context.Context) error {
	spanCtx, span := c.tracer.Start(ctx, "run_migrations",
		trace.WithAttributes(attribute.Int("db.pgc.cluster_size", int(c.Size()))))
	defer span.End()

	for _, inst := range c.uniqueInstances {
		if err := inst.RunMigrations(spanCtx); err != nil {
			span.RecordError(err)
			return fmt.Errorf("pgc: migration failed on %s: %w", inst.String(), err)
		}
	}
	return nil
}

func (c *pgCluster) Ping(ctx context.Context) error {
	for _, inst := range c.uniqueInstances {
		if err := inst.Ping(ctx); err != nil {
			return fmt.Errorf("pgc: ping failed on %s: %w", inst.String(), err)
		}
	}
	return nil
}

func (c *pgCluster) Close(ctx context.Context) error {
	var errs []error
	for _, inst := range c.uniqueInstances {
		errs = append(errs, inst.Close(ctx))
	}
	return errors.Join(errs...)
}

func (c *pgCluster) Size() byte { return byte(len(c.shards)) }
