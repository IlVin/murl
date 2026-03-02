package instance

import (
	"context"
	"fmt"
	"log/slog"
	"murl/internal/repository/pgc"
	"sync"

	"github.com/jackc/pgx/v5"
)

// Константы для подсистемы батчинга
const BatchBufSize int = 100
const BatchLimit int = 3

type BatchQuery struct {
	SQL    string
	Params []any
}

type BatchExecutor func(context.Context, *pgx.Batch) error

type BatchExec struct {
	pg           pgc.PgInstance
	batchLimiter chan bool
	batchWg      *sync.WaitGroup
	mu           sync.Mutex
	buf          []BatchQuery
}

func NewBatchExec(pg pgc.PgInstance, batchLimiter chan bool, batchWg *sync.WaitGroup) *BatchExec {
	if batchLimiter == nil {
		batchLimiter = make(chan bool, BatchLimit)
	}
	if batchWg == nil {
		batchWg = new(sync.WaitGroup)
	}
	return &BatchExec{
		pg:           pg,
		batchLimiter: batchLimiter,
		batchWg:      batchWg,
		buf:          make([]BatchQuery, 0, BatchBufSize),
	}
}

// Add добавляет SQL запрос в буфер и, по заполненности буфера, запускает batchExecutor
func (b *BatchExec) Add(q BatchQuery) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buf = append(b.buf, q)
	b.flush(BatchBufSize)
}

// Flush вызывает batchExecutor, если буфер SQL запросов заполнен не менее, чем на maxBufLen
func (b *BatchExec) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.flush(0)
}

// Flush вызывает batchExecutor, если буфер SQL запросов заполнен не менее, чем на maxBufLen
func (b *BatchExec) flush(maxBufLen int) {
	if len(b.buf) == 0 || len(b.buf) < maxBufLen {
		return
	}

	// Отстегиваем слайс
	execBuf := b.buf
	b.buf = make([]BatchQuery, 0, BatchBufSize)

	// Запускаем горутину
	b.batchWg.Add(1)
	go b.execQuery(execBuf)
}

// execQuery запускает batchExecutor
func (b *BatchExec) execQuery(buf []BatchQuery) {
	defer b.batchWg.Done()
	defer func() {
		if r := recover(); r != nil {
			slog.Error(
				"panic recovered",
				slog.Any("reason", r),
				slog.Int("not_exec_queries", len(buf)),
			)
		}
	}()

	// Подготавляваем батч
	batch := &pgx.Batch{}
	for i := range buf {
		batch.Queue(buf[i].SQL, buf[i].Params...)
	}

	// Кладем токен и блокируемся в очереди
	b.batchLimiter <- true
	defer func() { <-b.batchLimiter }()

	// Вызываем исполнятор батча
	err := b.pg.Tx(context.Background(), func(ctx context.Context, tx pgc.PgxTxIface) error {
		br := tx.SendBatch(ctx, batch)
		defer br.Close()
		for range batch.Len() {
			ct, err := br.Exec()
			if err != nil {
				return fmt.Errorf("batch execute fail (%s): %w", ct.String(), err)
			}
		}
		return nil
	})

	if err != nil {
		slog.Error("batch exec fail",
			slog.Any("err", err),
		)
	}
}
