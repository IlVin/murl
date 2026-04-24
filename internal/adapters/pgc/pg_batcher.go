package pgc

import (
	"context"
	"sync"
	"time"
)

const (
	// DefaultBatchLimit определяет, сколько запросов упаковывается в один сетевой пакет (roundtrip).
	DefaultBatchLimit = 50
	// DefaultResultsLimit определяет размер буфера канала результатов для обеспечения backpressure.
	DefaultResultsLimit = 1
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

// NewPgBatcher создает новый экземпляр пакетного исполнителя.
// Возвращает соответствующий тип в зависимости от того, возвращает ли запрос данные.
func NewPgBatcher[C comparable, T any](ctx context.Context, pg PgInstance, q *Query[T]) PgBatcher[T, C] {
	c, cancel := context.WithCancel(ctx)

	if q.HasReturns() {
		// Для запросов с возвратом данных: буфер DefaultResultsLimit
		b := &pgQueryBatcher[T, C]{
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

	// Для команд без возврата данных: буфер 1 (только для ошибки)
	b := &pgCommandBatcher[T, C]{
		pg:       pg,
		query:    q,
		ctx:      c,
		cancel:   cancel,
		requests: make(chan BatchEntry[C], DefaultBatchLimit),
		results:  make(chan BatchResult[T, C], 1), // буфер 1 для ошибки
		wg:       sync.WaitGroup{},
	}
	b.wg.Add(1)
	go b.worker()
	return b
}
