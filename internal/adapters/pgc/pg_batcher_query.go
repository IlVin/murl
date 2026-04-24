package pgc

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// pgQueryBatcher реализует PgBatcher для запросов, возвращающих данные (HasReturns == true)
type pgQueryBatcher[T any, C comparable] struct {
	pg       PgInstance
	query    *Query[T]
	ctx      context.Context
	cancel   context.CancelFunc
	requests chan BatchEntry[C]
	results  chan BatchResult[T, C]
	wg       sync.WaitGroup
}

// Requests возвращает канал для отправки заданий.
func (b *pgQueryBatcher[T, C]) Requests() chan<- BatchEntry[C] {
	return b.requests
}

// Results возвращает канал для чтения результатов.
func (b *pgQueryBatcher[T, C]) Results() <-chan BatchResult[T, C] {
	return b.results
}

// worker — основной цикл накопления и отправки пакетов для Query-батчера.
func (b *pgQueryBatcher[T, C]) worker() {
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

// execute выполняет физическую отправку пакета для Query-батчера.
func (b *pgQueryBatcher[T, C]) execute(toWork []BatchEntry[C], retryQueue *[]BatchEntry[C]) {
	argsMatrix := make([][]any, len(toWork))
	for i := range toWork {
		argsMatrix[i] = toWork[i].Args
	}

	stream := b.pg.SendBatch(b.ctx, b.query, argsMatrix)
	processed := 0
	var lastErr error

	for val, err := range stream {
		if err != nil {
			lastErr = err
			break
		}

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

// send отправляет результат в канал с гарантией доставки.
func (b *pgQueryBatcher[T, C]) send(res BatchResult[T, C]) {
	select {
	case b.results <- res:
	case <-b.ctx.Done():
	}
}

// Close закрывает входной канал и ожидает завершения обработки всех задач.
func (b *pgQueryBatcher[T, C]) Close() error {
	close(b.requests)
	b.wg.Wait()
	b.cancel()
	return nil
}
