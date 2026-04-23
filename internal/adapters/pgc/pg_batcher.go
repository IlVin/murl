package pgc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	// DefaultBatchLimit определяет, сколько запросов упаковывается в один сетевой пакет (roundtrip).
	DefaultBatchLimit = 50
	// DefaultResultsLimit определяет размер буфера канала результатов для обеспечения backpressure.
	DefaultResultsLimit = 1000
	// maxRetries определяет максимальное количество попыток для повторяемых (retryable) ошибок БД.
	maxRetries = 3
	// flushInterval определяет время ожидания перед отправкой неполного пакета.
	flushInterval = 10 * time.Millisecond
)

// BatchEntry связывает аргументы SQL-запроса с пользовательским контекстом.
// Тип C (comparable) используется для идентификации конкретного задания в потоке результатов.
type BatchEntry[C comparable] struct {
	Args     []any // SQL query parameters.
	Ctx      C     // User-defined context for identifying the result.
	attempts int
}

// BatchResult представляет результат выполнения отдельной операции в пакете.
type BatchResult[T any, C comparable] struct {
	Data          T     // Отсканированные данные типа T.
	Err           error // Ошибка, если операция завершилась неудачей.
	Ctx           C     // Исходный пользовательский контекст, связанный с этим результатом.
	IsLastInBatch bool  // Признак завершения логического пакета (sentinel flag).
}

// PgBatcher определяет интерфейс для типизированного конвейера (pipeline) базы данных.
// Позволяет отправлять запросы пачками, автоматически обрабатывая ретраи и тайм-ауты накопления.
type PgBatcher[T any, C comparable] interface {
	// Requests возвращает канал только для записи для отправки новых заданий.
	Requests() chan<- BatchEntry[C]
	// Results возвращает канал только для чтения для получения результатов операций.
	Results() <-chan BatchResult[T, C]
	// Close выполняет грациозное завершение: сбрасывает остатки очереди и дожидается завершения ретраев.
	Close() error
}

type pgBatcher[T any, C comparable] struct {
	pg     PgInstance
	query  *Query[T]
	ctx    context.Context
	cancel context.CancelFunc

	requests chan BatchEntry[C]
	results  chan BatchResult[T, C]
	wg       sync.WaitGroup
}

// NewPgBatcher создает новый экземпляр пакетного исполнителя.
// Тип C (Context) должен быть указан явно, тип T (Result) выводится из Query.
func NewPgBatcher[C comparable, T any](ctx context.Context, pg PgInstance, q *Query[T]) PgBatcher[T, C] {
	c, cancel := context.WithCancel(ctx)
	b := &pgBatcher[T, C]{
		pg:       pg,
		query:    q,
		ctx:      c,
		cancel:   cancel,
		requests: make(chan BatchEntry[C], DefaultBatchLimit),
		results:  make(chan BatchResult[T, C], DefaultResultsLimit),
		wg:       sync.WaitGroup{},
	}
	b.wg.Add(1)
	go b.worker()
	return b
}

// Requests возвращает канал для отправки заданий. Реализует PgBatcher.
func (b *pgBatcher[T, C]) Requests() chan<- BatchEntry[C] { return b.requests }

// Results возвращает канал для чтения результатов. Реализует PgBatcher.
func (b *pgBatcher[T, C]) Results() <-chan BatchResult[T, C] { return b.results }

// worker — основной цикл накопления и отправки пакетов.
func (b *pgBatcher[T, C]) worker() {
	defer b.wg.Done()
	defer close(b.results)

	batch := make([]BatchEntry[C], 0, DefaultBatchLimit)
	retryQueue := make([]BatchEntry[C], 0, DefaultBatchLimit)

	timer := time.NewTimer(flushInterval)
	defer timer.Stop()

	for {
		if len(retryQueue) > 0 && len(batch) < DefaultBatchLimit {
			take := min(DefaultBatchLimit-len(batch), len(retryQueue))
			batch = append(batch, retryQueue[:take]...)
			retryQueue = retryQueue[take:]
		}

		select {
		case <-b.ctx.Done():
			return
		case entry, ok := <-b.requests:
			if !ok {
				finalBatch := append(batch, retryQueue...)
				for len(finalBatch) > 0 {
					tempRetry := make([]BatchEntry[C], 0, len(finalBatch))
					b.execute(finalBatch, &tempRetry)
					finalBatch = tempRetry
				}
				return
			}
			batch = append(batch, entry)
			if len(batch) == 1 {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(flushInterval)
			}
			if len(batch) >= DefaultBatchLimit {
				b.execute(batch, &retryQueue)
				batch = batch[:0]
			}
		case <-timer.C:
			if len(batch) > 0 {
				b.execute(batch, &retryQueue)
				batch = batch[:0]
			}
		}
	}
}

// execute выполняет физическую отправку пакета в PgInstance и обрабатывает ошибки.
func (b *pgBatcher[T, C]) execute(toWork []BatchEntry[C], retryQueue *[]BatchEntry[C]) {
	argsMatrix := make([][]any, len(toWork))
	for i := range toWork {
		argsMatrix[i] = toWork[i].Args
	}

	stream := b.pg.SendBatch(b.ctx, b.query, argsMatrix)
	processed := 0
	var lastErr error
	hasReturns := b.query.HasReturns()

	for val, err := range stream {
		if err != nil {
			lastErr = err
			break
		}
		if hasReturns {
			res := BatchResult[T, C]{
				Ctx:           toWork[processed].Ctx,
				IsLastInBatch: processed == len(toWork)-1,
			}
			if ptr, ok := val.(*T); ok {
				res.Data = *ptr
			} else {
				res.Err = fmt.Errorf("pgc: batcher expected *%T, got %T", new(T), val)
			}
			b.send(res)
		}
		processed++
	}

	if lastErr != nil {
		// Если результаты никто не ждет (режим Command), логируем ошибку здесь,
		// иначе она может молча исчезнуть в методе send из-за select default.
		if !hasReturns {
			slog.Error("pgc: batch execution failed",
				"query", b.query.Name(),
				"err", lastErr)
		}
		remaining := toWork[processed:]
		if isRetryable(lastErr) && retryQueue != nil {
			for i, entry := range remaining {
				nextAttempt := entry.attempts + 1
				if nextAttempt < maxRetries {
					entry.attempts = nextAttempt
					*retryQueue = append(*retryQueue, entry)
				} else {
					b.send(BatchResult[T, C]{
						Err:           fmt.Errorf("retries exhausted: %w", lastErr),
						Ctx:           entry.Ctx,
						IsLastInBatch: i == len(remaining)-1,
					})
				}
			}
		} else {
			for i, entry := range remaining {
				b.send(BatchResult[T, C]{
					Err:           fmt.Errorf("final attempt failed: %w", lastErr),
					Ctx:           entry.Ctx,
					IsLastInBatch: i == len(remaining)-1,
				})
			}
		}
	}
}

// send отправляет результат в канал, учитывая контекст завершения.
func (b *pgBatcher[T, C]) send(res BatchResult[T, C]) {
	select {
	case b.results <- res:
	case <-b.ctx.Done():
	default:
	}
}

// Close закрывает входной канал и ожидает завершения обработки всех задач. Реализует PgBatcher.
func (b *pgBatcher[T, C]) Close() error {
	close(b.requests)
	b.wg.Wait()
	b.cancel()
	return nil
}
