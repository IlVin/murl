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
	"go.opentelemetry.io/otel/trace"
	traceNoop "go.opentelemetry.io/otel/trace/noop"
)

const (
	// otelNameCqrs - имя подсистемы для инструментирования OpenTelemetry.
	otelNameCqrs = "cqrs"

	// retryBackoff - длительность ожидания перед повторной попыткой записи на мастер-узле.
	retryBackoff = 100 * time.Millisecond

	// maxMasterAttempts - максимальное количество попыток для операций записи на мастере.
	maxMasterAttempts = 2
)

// cqrsConnector реализует PgInstance с поддержкой разделения на Master/Replica.
// Обеспечивает автоматическое распределение запросов и логику ретраев на уровне драйвера.
// Это реализация паттерна Декоратор, добавляющая CQRS-возможности к любой PgInstance.
type cqrsConnector struct {
	master   PgInstance
	replicas []PgInstance
	index    atomic.Uint64

	meter  metric.Meter
	tracer trace.Tracer
	logger *slog.Logger

	// Метрики
	mRetries metric.Int64Counter
}

// NewCQRSConnector создаёт новый экземпляр CQRS-прокси драйвера.
// Если переданы реплики, read-only запросы будут распределяться между ними алгоритмом Round-Robin.
// Write-запросы всегда направляются на мастер-узел.
//
// Пример использования:
//
//	master := pgc.NewPgConnector(ctx, "postgres://master:5432/db")
//	replica1 := pgc.NewPgConnector(ctx, "postgres://replica1:5432/db")
//	replica2 := pgc.NewPgConnector(ctx, "postgres://replica2:5432/db")
//	db := pgc.NewCQRSConnector(master, replica1, replica2)
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

// initMetrics инициализирует метрики OpenTelemetry для коннектора.
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

// getNodeOrder возвращает упорядоченный список узлов для выполнения запроса.
//
// Для read-only запросов: возвращает все живые реплики (в порядке Round-Robin), затем мастер.
// Для write-запросов: возвращает мастер дважды (разрешая одну повторную попытку).
//
// Такой порядок гарантирует:
//   - Нагрузка на чтение распределяется между доступными репликами
//   - Запись всегда идёт на мастер с возможностью одного ретрая
//   - Отключённые реплики автоматически пропускаются
func (m *cqrsConnector) getNodeOrder(isRO bool) []PgInstance {
	if !isRO {
		// Write-операции: только мастер, с возможностью ретрая
		return []PgInstance{m.master, m.master}
	}

	// Read-операции: сначала живые реплики (Round-Robin), затем мастер
	var nodes []PgInstance
	if len(m.replicas) > 0 {
		n64 := uint64(len(m.replicas))
		// Round-Robin: начинаем со следующей реплики
		startIdx := m.index.Add(1) % n64

		for i := uint64(0); i < n64; i++ {
			replica := m.replicas[(startIdx+i)%n64]
			if replica.IsOnline() {
				nodes = append(nodes, replica)
			}
		}
	}

	// Мастер всегда последний (даже если оффлайн — вызов упадёт быстро)
	return append(nodes, m.master)
}

// Fetch реализует потоковое чтение с автоматическим ретраем на повторимых ошибках.
//
// Стратегия ретрая:
//   - Если первый узел вернул ошибку ДО передачи хотя бы одной строки, и ошибка повторимая,
//     запрос повторяется на следующем узле из очереди.
//   - Если хотя бы одна строка уже передана, ретрай НЕ ВЫПОЛНЯЕТСЯ во избежание дублирования данных.
//   - Для read-only запросов: ретрай на репликах, затем на мастере.
//   - Для write-запросов: ретрай только на мастере (с бэкоффом).
//
// Функция возвращает итератор, выдающий строки до возникновения ошибки или прерывания пользователем.
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
		nodes := m.getNodeOrder(isRO)

		var lastErr error
		var firstRowReceived bool

		for attempt, db := range nodes {
			// Логируем ретраи и применяем бэкофф для мастера
			if attempt > 0 {
				if isRO {
					m.recordRetry(ctx, q.Name(), "fetch_ro_retry")
				} else {
					m.recordRetry(ctx, q.Name(), "fetch_retry_master")
					select {
					case <-time.After(retryBackoff):
					case <-ctx.Done():
						span.SetStatus(codes.Error, ctx.Err().Error())
						var zero any
						yield(zero, ctx.Err())
						return
					}
				}
			}

			// Выполняем запрос на текущем узле
			innerErr := func() error {
				for v, err := range db.Fetch(ctx, q, args...) {
					if err != nil {
						lastErr = err
						// Ретрай возможен ТОЛЬКО если: строки ещё не переданы И ошибка повторимая И есть ещё узлы
						if !firstRowReceived && isRetryable(err) && attempt < len(nodes)-1 {
							return nil // Выходим из внутреннего цикла, пробуем следующий узел
						}
						return err // Возвращаем финальную ошибку
					}

					if !firstRowReceived {
						firstRowReceived = true
					}
					if !yield(v, nil) {
						span.SetStatus(codes.Ok, "прерывание пользователем")
						return nil
					}
				}
				return nil
			}()

			if innerErr != nil {
				span.RecordError(innerErr)
				span.SetStatus(codes.Error, innerErr.Error())
				var zero any
				yield(zero, innerErr)
				return
			}

			// Если мы здесь из-за break (ретрай), продолжаем со следующим узлом
			if lastErr != nil && !firstRowReceived && attempt < len(nodes)-1 {
				continue
			}

			// Успех
			span.SetStatus(codes.Ok, "")
			return
		}

		// Все попытки исчерпаны с ошибкой
		if lastErr != nil {
			span.RecordError(lastErr)
			span.SetStatus(codes.Error, lastErr.Error())
			var zero any
			yield(zero, lastErr)
		}
	}
}

// FetchRow получает одну строку с поддержкой ретраев.
//
// В отличие от Fetch, эта операция всегда безопасна для ретрая, потому что результат
// становится известен только после полного завершения операции (нет частичной передачи данных).
//
// Для read-only запросов: сначала реплики (Round-Robin), затем мастер.
// Для write-запросов: только мастер.
// Для повторимых ошибок: переход к следующему узлу в порядке очереди.
// Для неповторимых ошибок или исчерпания узлов: немедленный возврат ошибки.
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
	nodes := m.getNodeOrder(isRO)

	var lastErr error
	for attempt, db := range nodes {
		if attempt > 0 {
			m.recordRetry(ctx, q.Name(), "fetch_row")
			if !isRO {
				select {
				case <-time.After(retryBackoff):
				case <-ctx.Done():
					span.SetStatus(codes.Error, ctx.Err().Error())
					return nil, ctx.Err()
				}
			}
		}

		res, errFetchRow := db.FetchRow(ctx, q, args...)
		if errFetchRow == nil {
			span.SetStatus(codes.Ok, "")
			return res, nil
		}

		if !isRetryable(errFetchRow) || attempt == len(nodes)-1 {
			span.RecordError(errFetchRow)
			span.SetStatus(codes.Error, errFetchRow.Error())
			return nil, errFetchRow
		}
		lastErr = errFetchRow
	}

	span.RecordError(lastErr)
	span.SetStatus(codes.Error, lastErr.Error())
	return nil, lastErr
}

// Exec выполняет команду изменения данных (INSERT/UPDATE/DELETE).
//
// Write-операции ВСЕГДА направляются только на мастер-узел, никогда на реплики.
// Поддерживает до maxMasterAttempts (2) попыток с бэкоффом для повторимых ошибок.
// Возвращает количество затронутых строк или ошибку.
func (m *cqrsConnector) Exec(ctx context.Context, q PgQuery, args ...any) (int64, error) {
	ctx, span := m.tracer.Start(ctx, q.Name(),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation.name", "exec"),
			attribute.String("db.query.text", q.SQL()),
		))
	defer span.End()

	for attempt := 0; attempt < maxMasterAttempts; attempt++ {
		if attempt > 0 {
			m.recordRetry(ctx, q.Name(), "exec")
			select {
			case <-time.After(retryBackoff):
			case <-ctx.Done():
				span.SetStatus(codes.Error, ctx.Err().Error())
				return 0, ctx.Err()
			}
		}

		rows, err := m.master.Exec(ctx, q, args...)
		if err == nil {
			span.SetAttributes(attribute.Int64("db.response.rows_affected", rows))
			span.SetStatus(codes.Ok, "")
			return rows, nil
		}
		if !isRetryable(err) || attempt == maxMasterAttempts-1 {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return 0, err
		}
	}
	return 0, nil
}

// SendBatch выполняет пакет однотипных запросов.
//
// Стратегия ретрая следует тем же правилам, что и у Fetch:
//   - Безопасно ретраить ТОЛЬКО если ещё не передано ни одной строки
//   - После передачи первой строки любая ошибка считается финальной
//
// Для read-only пакетов: сначала реплики (Round-Robin), затем мастер.
// Для write-пакетов: только мастер (с ретраем).
func (m *cqrsConnector) SendBatch(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
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
		nodes := m.getNodeOrder(isRO)

		var lastErr error
		var firstRowReceived bool

		for attempt, db := range nodes {
			if attempt > 0 {
				m.recordRetry(ctx, q.Name(), "batch")
				if !isRO {
					select {
					case <-time.After(retryBackoff):
					case <-ctx.Done():
						span.SetStatus(codes.Error, ctx.Err().Error())
						var zero any
						yield(zero, ctx.Err())
						return
					}
				}
			}

			innerErr := func() error {
				for v, err := range db.SendBatch(ctx, q, args) {
					if err != nil {
						lastErr = err
						if !firstRowReceived && isRetryable(err) && attempt < len(nodes)-1 {
							return nil // Ретрай на следующем узле
						}
						return err
					}

					if !firstRowReceived {
						firstRowReceived = true
					}
					if !yield(v, nil) {
						span.SetStatus(codes.Ok, "прерывание пользователем")
						return nil
					}
				}
				return nil
			}()

			if innerErr != nil {
				span.RecordError(innerErr)
				span.SetStatus(codes.Error, innerErr.Error())
				var zero any
				yield(zero, innerErr)
				return
			}

			if lastErr != nil && !firstRowReceived && attempt < len(nodes)-1 {
				continue
			}

			span.SetStatus(codes.Ok, "")
			return
		}

		if lastErr != nil {
			span.RecordError(lastErr)
			span.SetStatus(codes.Error, lastErr.Error())
			var zero any
			yield(zero, lastErr)
		}
	}
}

// recordRetry фиксирует событие ретрая в метриках OpenTelemetry.
func (m *cqrsConnector) recordRetry(ctx context.Context, name, op string) {
	m.mRetries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("db.query.name", name),
		attribute.String("db.operation.name", op),
	))
}

// IsOnline возвращает true, если мастер-узел доступен (предохранитель замкнут).
// Коннектор считается оффлайн, если мастер недоступен.
func (m *cqrsConnector) IsOnline() bool {
	return m.master.IsOnline()
}

// RunMigrations запускает миграции схемы базы данных.
// Эта операция всегда выполняется строго на мастер-узле.
func (m *cqrsConnector) RunMigrations(ctx context.Context) error {
	return m.master.RunMigrations(ctx)
}

// Ping проверяет физическую доступность мастер-узла.
func (m *cqrsConnector) Ping(ctx context.Context) error {
	return m.master.Ping(ctx)
}

// Close корректно закрывает все соединения: сначала мастера, затем всех реплик.
// Возвращает ошибку, если мастер не закрылся (ошибки реплик логируются, но игнорируются).
func (m *cqrsConnector) Close(ctx context.Context) error {
	err := m.master.Close(ctx)
	for _, r := range m.replicas {
		_ = r.Close(ctx)
	}
	return err
}

// String возвращает строковое представление коннектора для отладки.
// Пример: "<CQRSConnector>{Master:localhost:5432/mydb, Replicas:2}"
func (m *cqrsConnector) String() string {
	return fmt.Sprintf("<CQRSConnector>{Master:%s, Replicas:%d}", m.master.String(), len(m.replicas))
}
