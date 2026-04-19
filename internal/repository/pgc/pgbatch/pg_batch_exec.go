package pgbatch

import (
	"context"
	"fmt"
	"log/slog"
	"murl/internal/repository/pgc"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:generate $GOPATH/bin/mockgen                           -destination=pg_batch_exec_pgx_mock_test.go         -package=$GOPACKAGE github.com/jackc/pgx/v5 Tx,Row,BatchResults
//go:generate $GOPATH/bin/mockgen -source=../pg_instance.go -destination=pg_batch_exec_pg_instance_mock_test.go -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=$GOFILE           -destination=pg_batch_exec_mock_test.go             -package=$GOPACKAGE

// Константы для подсистемы батчинга
const BatchBufSize int = 100
const BatchLimit int = 3
const BatchTimeout time.Duration = 500 * time.Millisecond

type BatchQuery struct {
	SQL    string
	Params []any
}

type BatchExec struct {
	pg           pgc.PgInstance
	batchLimiter chan bool
	batchWg      *sync.WaitGroup
	mu           sync.Mutex
	buf          []*BatchQuery

	ctx       context.Context
	cancel    func()
	lastFlush atomic.Int64 // Храним UnixNano последнего сброса
}

func NewBatchExec(pg pgc.PgInstance, batchLimiter chan bool, batchWg *sync.WaitGroup) *BatchExec {
	if batchLimiter == nil {
		batchLimiter = make(chan bool, BatchLimit)
	}
	if batchWg == nil {
		batchWg = new(sync.WaitGroup)
	}

	ctx, cancel := context.WithCancel(context.Background())

	b := &BatchExec{
		pg:           pg,
		batchLimiter: batchLimiter,
		batchWg:      batchWg,
		buf:          make([]*BatchQuery, 0, BatchBufSize),
		ctx:          ctx,
		cancel:       cancel,
	}

	b.lastFlush.Store(time.Now().UnixNano())

	// Запуск фонового сброса по тикеру
	go b.tickerLoop()

	return b
}

func (b *BatchExec) tickerLoop() {
	ticker := time.NewTicker(BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Если с момента последнего Flush прошло больше времени, чем BatchTimeout
			if time.Since(time.Unix(0, b.lastFlush.Load())) >= BatchTimeout {
				b.Flush()
			}
		case <-b.ctx.Done():
			return
		}
	}
}

// Close останавливает тикер и сбрасывает остатки (Graceful shutdown)
func (b *BatchExec) Close() {
	b.cancel()
	b.Flush()
}

// Add добавляет SQL запрос в буфер и, по заполненности буфера, запускает batchExecutor
func (b *BatchExec) Add(SQL string, prms []any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buf = append(b.buf, &BatchQuery{
		SQL:    SQL,
		Params: prms,
	})
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
	b.buf = make([]*BatchQuery, 0, BatchBufSize)

	// Обновляем метку времени последнего сброса
	b.lastFlush.Store(time.Now().UnixNano())

	// Запускаем горутину
	b.batchWg.Add(1)
	go b.execQuery(execBuf)
}

// execQuery запускает batchExecutor
func (b *BatchExec) execQuery(buf []*BatchQuery) {
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
	execCtx := context.WithoutCancel(b.ctx)
	err := b.pg.Tx(execCtx, func(ctx context.Context, tx pgc.PgxTxIface) error {
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
