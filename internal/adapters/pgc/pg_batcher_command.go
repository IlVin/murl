package pgc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// pgCommandBatcher реализует PgBatcher для команд, не возвращающих данные (HasReturns == false)
// Особенности:
//   - При ошибке отправляет один результат с Err в канал Results
//   - При успехе не отправляет ничего (канал закрывается пустым)
//   - Канал Results буферизированный (размер 1), чтобы не блокировать worker при отсутствии читателя
type pgCommandBatcher[T any, C comparable] struct {
	pg       PgInstance
	query    *Query[T]
	ctx      context.Context
	cancel   context.CancelFunc
	requests chan BatchEntry[C]
	results  chan BatchResult[T, C]
	wg       sync.WaitGroup
	closed   bool
	mu       sync.Mutex
}

// Requests возвращает канал для отправки заданий.
func (b *pgCommandBatcher[T, C]) Requests() chan<- BatchEntry[C] {
	return b.requests
}

// Results возвращает канал для чтения результатов.
// В Command-режиме канал буферизированный (размер 1):
//   - При успехе канал закрывается без отправки результатов
//   - При ошибке отправляется один результат с Err и канал закрывается
func (b *pgCommandBatcher[T, C]) Results() <-chan BatchResult[T, C] {
	return b.results
}

// worker — основной цикл накопления и отправки пакетов для Command-батчера.
func (b *pgCommandBatcher[T, C]) worker() {
	defer b.wg.Done()
	defer b.closeResults()

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
					b.execute(finalBatch, &retryQueue)
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

// execute выполняет физическую отправку пакета для Command-батчера.
// Специфика PostgreSQL batch: после первой ошибки все последующие запросы тоже вернут ошибку.
// Поэтому отправляем только один результат с ошибкой (или ничего при успехе).
func (b *pgCommandBatcher[T, C]) execute(toWork []BatchEntry[C], retryQueue *[]BatchEntry[C]) {
	argsMatrix := make([][]any, len(toWork))
	for i := range toWork {
		argsMatrix[i] = toWork[i].Args
	}

	stream := b.pg.SendBatch(b.ctx, b.query, argsMatrix)

	var lastErr error
	hasError := false

	// Читаем stream до первой ошибки
	for _, err := range stream {
		if err != nil {
			lastErr = err
			hasError = true
			break
		}
	}

	if hasError {
		slog.Error("pgc: command batch execution failed",
			"query", b.query.Name(),
			"batch_size", len(toWork),
			"err", lastErr)

		// Проверяем, можно ли ретраить
		if isRetryable(lastErr) && retryQueue != nil {
			// Проверяем, не превышен ли лимит ретраев для первого элемента
			if toWork[0].attempts+1 < maxRetries {
				// Увеличиваем счётчик попыток для всех элементов
				for i := range toWork {
					toWork[i].attempts++
				}
				*retryQueue = append(*retryQueue, toWork...)
				return
			}
		}

		// Отправляем один результат с ошибкой
		// Используем Ctx первого запроса как идентификатор батча
		b.send(BatchResult[T, C]{
			Err:           fmt.Errorf("batch failed: %w", lastErr),
			Ctx:           toWork[0].Ctx,
			IsLastInBatch: true,
		})
	}
	// При успехе ничего не отправляем, канал просто закроется
}

// send отправляет результат в канал с гарантией доставки.
// Использует буферизированный канал (размер 1), чтобы не блокироваться.
func (b *pgCommandBatcher[T, C]) send(res BatchResult[T, C]) {
	select {
	case b.results <- res:
	case <-b.ctx.Done():
	}
}

// closeResults безопасно закрывает канал результатов.
func (b *pgCommandBatcher[T, C]) closeResults() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		close(b.results)
		b.closed = true
	}
}

// Close закрывает входной канал и ожидает завершения обработки всех задач.
func (b *pgCommandBatcher[T, C]) Close() error {
	close(b.requests)
	b.wg.Wait()
	b.cancel()
	return nil
}
