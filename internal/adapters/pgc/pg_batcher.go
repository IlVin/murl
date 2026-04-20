package pgc

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	// DefaultBatchLimit defines how many queries are packed into a single network roundtrip.
	DefaultBatchLimit = 50
	// DefaultResultsLimit defines the buffer size for results to provide backpressure.
	DefaultResultsLimit = 1000
	// maxRetries defines the maximum number of attempts for retryable database errors.
	maxRetries = 3
	// flushInterval defines how long to wait before sending a partial batch.
	flushInterval = 10 * time.Millisecond
)

// BatchEntry links SQL arguments with a user-defined context.
type BatchEntry[C comparable] struct {
	Args     []any // SQL query parameters.
	Ctx      C     // User-defined context for identifying the result.
	attempts int
}

// BatchResult represents the outcome of a single operation in a batch.
type BatchResult[T any, C comparable] struct {
	Data          T     // Scanned result data of type T.
	Err           error // Error if the operation failed.
	Ctx           C     // Original user context linked to this result.
	IsLastInBatch bool  // Sentinel flag signaling the completion of a logical batch.
}

// PgBatcher defines the interface for a typed streaming database pipeline.
type PgBatcher[T any, C comparable] interface {
	// Requests returns the write-only channel for submitting new jobs.
	Requests() chan<- BatchEntry[C]
	// Results returns the read-only channel for consuming operation outcomes.
	Results() <-chan BatchResult[T, C]
	// Close performs a graceful shutdown, flushing all pending tasks and retries.
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

// NewBatcher creates a new PgBatcher instance.
// Type C (Context) must be provided explicitly, while T (Result) is inferred from the Query.
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

// Requests implements the PgBatcher interface.
func (b *pgBatcher[T, C]) Requests() chan<- BatchEntry[C] { return b.requests }

// Results implements the PgBatcher interface.
func (b *pgBatcher[T, C]) Results() <-chan BatchResult[T, C] { return b.results }

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
				if len(finalBatch) > 0 {
					b.execute(finalBatch, nil)
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

func (b *pgBatcher[T, C]) send(res BatchResult[T, C]) {
	select {
	case b.results <- res:
	case <-b.ctx.Done():
	default:
	}
}

// Close implements the PgBatcher interface.
func (b *pgBatcher[T, C]) Close() error {
	close(b.requests)
	b.wg.Wait()
	b.cancel()
	return nil
}
