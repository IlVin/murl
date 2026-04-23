package pgc

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricNoop "go.opentelemetry.io/otel/metric/noop"
	traceNoop "go.opentelemetry.io/otel/trace/noop"

	"go.opentelemetry.io/otel/trace"
)

const otelNameCqrs = "cqrs"

// cqrsConnector реализует PgInstance с поддержкой разделения на Master/Replica.
// Обеспечивает автоматическое распределение запросов и логику ретраев на уровне драйвера.
type cqrsConnector struct {
	master   PgInstance
	replicas []PgInstance
	index    atomic.Uint64

	meter  metric.Meter
	tracer trace.Tracer
	logger *slog.Logger

	// Метрики (стандартные имена для Prometheus/Grafana)
	mRetries metric.Int64Counter
}

// NewCQRSConnector создает новый инстанс прокси-драйвера.
// Если переданы реплики, запросы, помеченные как ReadOnly, будут распределяться между ними Round-Robin.
func NewCQRSConnector(master PgInstance, replicas ...PgInstance) PgInstance {
	m := &cqrsConnector{
		master:   master,
		replicas: replicas,
		meter:    metricNoop.NewMeterProvider().Meter(getInstrumentationName(otelNameCqrs)),
		tracer:   traceNoop.NewTracerProvider().Tracer(getInstrumentationName(otelNameCqrs)),
		logger:   slog.Default(),
	}

	m.initMetrics()

	return m
}

func (m *cqrsConnector) initMetrics() {
	m.mRetries, _ = m.meter.Int64Counter("db.client.retries.total",
		metric.WithDescription("Total number of query retries across all replicas"))
}

// WithTracerProvider пробрасывает TracerProvider во все вложенные инстансы (Master и Replicas).
func (m *cqrsConnector) WithTracerProvider(tp trace.TracerProvider) PgInstance {
	if tp != nil {
		m.tracer = tp.Tracer(getInstrumentationName(otelNameCqrs))
	}
	m.master.WithTracerProvider(tp)
	for _, r := range m.replicas {
		r.WithTracerProvider(tp)
	}
	return m
}

// WithMeterProvider пробрасывает MeterProvider во все вложенные инстансы и переинициализирует метрики.
func (m *cqrsConnector) WithMeterProvider(mp metric.MeterProvider) PgInstance {
	if mp != nil {
		m.meter = mp.Meter(getInstrumentationName(otelNameCqrs))
		m.initMetrics()
	}

	m.master.WithMeterProvider(mp)
	for _, r := range m.replicas {
		r.WithMeterProvider(mp)
	}
	return m
}

// WithSlogHandler устанавливает общий обработчик логов для всей связки CQRS.
func (m *cqrsConnector) WithSlogHandler(h slog.Handler) PgInstance {
	if h != nil {
		m.logger = slog.New(h)
	}
	m.master.WithSlogHandler(h)
	for _, r := range m.replicas {
		r.WithSlogHandler(h)
	}
	return m
}

// Fetch реализует потоковое чтение. Если запрос ReadOnly, при ошибке соединения
// Fetch автоматически попробует переключиться на другую реплику или Master,
// при условии, что итерация еще не началась (данные не начали передаваться в yield).
func (m *cqrsConnector) Fetch(ctx context.Context, q PgQuery, args ...any) iter.Seq2[any, error] {
	ctx, span := m.tracer.Start(ctx, q.Name(),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation.name", "fetch"),
			attribute.String("db.query.text", q.SQL()),
		))

	return func(yield func(any, error) bool) {
		defer span.End()
		isRO := q.IsReadOnly()

		maxAttempts := 1
		if isRO && len(m.replicas) > 0 {
			maxAttempts = len(m.replicas) + 1
		}

		var lastErr error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				m.recordRetry(ctx, q.Name(), "fetch")
				if !isRO {
					time.Sleep(100 * time.Millisecond)
				}
			}

			db := m.pickNext(isRO, attempt)
			startedYielding := false
			currentAttemptFailed := false

			// ВНУТРЕННИЙ цикл по результатам конкретной ноды
			for v, err := range db.Fetch(ctx, q, args...) {
				if err != nil {
					lastErr = err
					currentAttemptFailed = true
					break // Выходим из range этого инстанса, но НЕ из цикла попыток
				}

				// Если данных нет, но yield вернул false — пользователь сделал break
				if !yield(v, nil) {
					return
				}
				startedYielding = true
			}

			// Если в этой попытке ошибок не было — мы закончили успешно
			if !currentAttemptFailed {
				span.SetStatus(codes.Ok, "")
				return
			}

			// Если ошибка ЕСТЬ, но мы уже начали отдавать данные (startedYielding == true),
			// или ошибка не ретраябельна, или попытки кончились — выходим из цикла попыток.
			if startedYielding || !isRetryable(lastErr) || attempt == maxAttempts-1 {
				break
			}

			// Если мы здесь — идем на следующую итерацию (ретрай),
			// НИЧЕГО не сообщая в yield. Для потребителя это просто пауза в стриме.
		}

		// Только когда мы ВЫШЛИ из цикла всех попыток и у нас осталась ошибка — отдаем её.
		if lastErr != nil {
			span.RecordError(lastErr)
			span.SetStatus(codes.Error, lastErr.Error())
			var zero any
			yield(zero, lastErr)
		}
	}
}

// FetchRow реализует получение одной строки с поддержкой ретраев на реплики.
func (m *cqrsConnector) FetchRow(ctx context.Context, q PgQuery, args ...any) (res any, err error) {
	ctx, span := m.tracer.Start(ctx, q.Name(),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation.name", "fetch_row"),
			attribute.String("db.query.text", q.SQL()),
		))
	defer span.End()

	isRO := q.IsReadOnly()
	maxAttempts := 1
	if isRO && len(m.replicas) > 0 {
		maxAttempts = len(m.replicas) + 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			m.recordRetry(ctx, q.Name(), "fetch_row")
		}

		db := m.pickNext(isRO, attempt)
		res, errFetchRow := db.FetchRow(ctx, q, args...)
		if errFetchRow == nil {
			span.SetStatus(codes.Ok, "")
			return res, nil
		}

		if !isRetryable(errFetchRow) || attempt == maxAttempts-1 {
			span.RecordError(errFetchRow)
			span.SetStatus(codes.Error, errFetchRow.Error())
			return nil, errFetchRow
		}
		err = errFetchRow
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return nil, err
}

// Exec выполняет команду изменения данных (INSERT/UPDATE/DELETE).
// Операция всегда направляется на Master-узел. Поддерживает до 2-х попыток при сетевых сбоях.
func (m *cqrsConnector) Exec(ctx context.Context, q PgQuery, args ...any) (int64, error) {
	ctx, span := m.tracer.Start(ctx, q.Name(),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation.name", "exec"),
			attribute.String("db.query.text", q.SQL()),
		))
	defer span.End()

	// Для Exec (Write) делаем максимум 2 попытки только на Master
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			m.recordRetry(ctx, q.Name(), "exec")
			time.Sleep(100 * time.Millisecond) // Backoff
		}

		rows, err := m.master.Exec(ctx, q, args...)
		if err == nil {
			span.SetAttributes(attribute.Int64("db.response.rows_affected", rows))
			span.SetStatus(codes.Ok, "")
			return rows, nil
		}
		if !isRetryable(err) || attempt == 1 {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return 0, err
		}
	}
	return 0, nil
}

// SendBatch выполняет пакет запросов. Если запрос помечен как ReadOnly, пакет может быть
// распределен на реплики. Реализует "прозрачный" ретрай: если одна нода вернула сетевую ошибку
// до начала передачи данных, коннектор попробует выполнить весь пакет на другой ноде.
func (m *cqrsConnector) SendBatch(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
	// Родительский спан для всей цепочки попыток
	ctx, span := m.tracer.Start(ctx, q.Name(),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation.name", "batch"),
			attribute.String("db.query.text", q.SQL()),
			attribute.Int("db.pgc.batch_size", len(args)),
			attribute.Bool("db.pgc.is_readonly", q.IsReadOnly()),
		))

	return func(yield func(any, error) bool) {
		defer span.End()
		isRO := q.IsReadOnly()

		maxAttempts := 1
		if isRO && len(m.replicas) > 0 {
			maxAttempts = len(m.replicas) + 1
		} else {
			maxAttempts = 2 // Только Master (2 попытки)
		}

		var lastErr error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				m.recordRetry(ctx, q.Name(), "batch")
				if !isRO {
					time.Sleep(100 * time.Millisecond) // Backoff для Master
				}
			}

			db := m.pickNext(isRO, attempt)

			startedYielding := false
			currentAttemptFailed := false

			// Внутренний цикл по результатам конкретной попытки на конкретном узле
			for v, err := range db.SendBatch(ctx, q, args) {
				if err != nil {
					lastErr = err
					currentAttemptFailed = true
					break // Выходим из итератора текущей ноды
				}

				// Если данных нет, но yield вернул false — пользователь сделал break в своем цикле
				if !yield(v, nil) {
					span.SetStatus(codes.Ok, "user break")
					return
				}
				startedYielding = true
			}

			// Если попытка прошла БЕЗ ошибок — мы закончили успешно.
			// Для потребителя это выглядит как один непрерывный поток.
			if !currentAttemptFailed {
				span.SetStatus(codes.Ok, "")
				return
			}

			// Если ошибка ЕСТЬ, проверяем: можно ли ретраить?
			// Если мы уже отдали хотя бы одну строку (startedYielding == true),
			// ретрай ОПАСЕН, так как мы не можем "отмотать" итератор назад.
			if startedYielding || !isRetryable(lastErr) || attempt == maxAttempts-1 {
				break
			}

			// Если мы здесь — мы "проглатываем" ошибку и идем на новую попытку (continue)
		}

		// Если мы вышли из цикла попыток и у нас осталась финальная ошибка — отдаем её.
		if lastErr != nil {
			span.RecordError(lastErr)
			span.SetStatus(codes.Error, lastErr.Error())
			var zero any
			yield(zero, lastErr)
		}
	}
}

// pickNext выбирает узел для выполнения запроса.
// Реализует Round-Robin для реплик с учетом их доступности (IsOnline).
func (m *cqrsConnector) pickNext(isRO bool, attempt int) PgInstance {
	n := len(m.replicas)

	// Если это НЕ Read-Only, или реплик нет, или мы УЖЕ перебрали все реплики
	// (attempt >= n), то идем в Master.
	if !isRO || n == 0 || attempt >= n {
		return m.master
	}

	n64 := uint64(n)
	// Атомарный инкремент для балансировки + сдвиг на номер попытки
	startIdx := (m.index.Add(1) + uint64(attempt)) % n64

	for i := uint64(0); i < n64; i++ {
		replica := m.replicas[(startIdx+i)%n64]
		if replica.IsOnline() {
			return replica
		}
	}

	// Если живых реплик не нашлось
	return m.master
}

// recordRetry фиксирует факт ретрая в метриках OpenTelemetry.
func (m *cqrsConnector) recordRetry(ctx context.Context, name, op string) {
	m.mRetries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("db.query.name", name),
		attribute.String("db.operation.name", op),
	))
}

// IsOnline возвращает признак работоспособности мастер-узла.
// Если мастер отключен предохранителем (Circuit Breaker), коннектор считается оффлайн.
func (m *cqrsConnector) IsOnline() bool { return m.master.IsOnline() }

// RunMigrations запускает процесс миграции схемы базы данных.
// Операция всегда выполняется строго на мастер-узле.
func (m *cqrsConnector) RunMigrations(ctx context.Context) error { return m.master.RunMigrations(ctx) }

// Ping проверяет физическую доступность мастер-узла.
func (m *cqrsConnector) Ping(ctx context.Context) error { return m.master.Ping(ctx) }

// Close выполняет каскадное закрытие всех соединений: сначала мастера,
// затем всех подключенных реплик. Возвращает ошибку, если мастер закрылся со сбоем.
func (m *cqrsConnector) Close(ctx context.Context) error {
	err := m.master.Close(ctx)
	for _, r := range m.replicas {
		_ = r.Close(ctx)
	}
	return err
}

// String реализует интерфейс fmt.Stringer, возвращая информацию
// о хосте мастера и количестве доступных реплик.
func (m *cqrsConnector) String() string {
	return fmt.Sprintf("<CQRSConnector>{Master:%s, Replicas:%d}", m.master.String(), len(m.replicas))
}
