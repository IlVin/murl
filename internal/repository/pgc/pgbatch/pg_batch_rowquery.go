package pgbatch

import (
	"context"
	"log/slog"
	"murl/internal/repository/pgc"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

const BatchRowQueryTimeout = 500 * time.Millisecond

// const BatchLimit int = 3

type BRQuery struct {
	SQL      string
	Params   []any
	Results  []any
	Metadata any
	Err      error
}

type bShard struct {
	pg        pgc.PgInstance
	brQueries []*BRQuery
}

type PgBatchRowQuery struct {
	mu             sync.Mutex
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         func()
	chOut          chan *BRQuery
	shards         map[string]*bShard
	chBatchLimiter chan bool
	lastFlush      atomic.Int64
}

func NewPgBatchRowQuery() *PgBatchRowQuery {
	ctx, cancel := context.WithCancel(context.Background())
	b := &PgBatchRowQuery{
		mu:             sync.Mutex{},
		wg:             sync.WaitGroup{},
		ctx:            ctx,
		cancel:         cancel,
		chBatchLimiter: make(chan bool, BatchLimit),
		shards:         make(map[string]*bShard),
	}

	b.lastFlush.Store(time.Now().UnixNano())
	b.chOut = make(chan *BRQuery, 3*BatchBufSize)

	// Запуск сервисной горутины
	go func(b *PgBatchRowQuery) {
		ticker := time.NewTicker(BatchRowQueryTimeout)
		defer func() {
			ticker.Stop()
			b.Flush()      // 1. Выталкиваем последние данные из слайса
			b.Wait()       // 2. Ждем завершения всех горутин query (БД)
			close(b.chOut) // 3. Закрываем выходной канал
			b.cancel()
		}()

		for {
			select {
			case <-ticker.C:
				if time.Since(time.Unix(0, b.lastFlush.Load())) >= BatchRowQueryTimeout {
					b.Flush()
				}
			case <-b.ctx.Done():
				return
			}
		}
	}(b)

	return b
}

func (b *PgBatchRowQuery) Push(pg pgc.PgInstance, q *BRQuery) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sID := pg.String()
	shrd, ok := b.shards[sID]
	if !ok || shrd == nil {
		shrd = newShard(pg)
		b.shards[sID] = shrd
	}
	shrd.brQueries = append(shrd.brQueries, q)

	// ПРОВЕРЯЕМ ТОЛЬКО ТЕКУЩИЙ ШАРД
	b.launchQuery(sID, shrd, BatchBufSize)
}

func (b *PgBatchRowQuery) C() <-chan *BRQuery {
	return b.chOut
}

func (b *PgBatchRowQuery) Close() {
	b.cancel()
}

func (b *PgBatchRowQuery) Wait() {
	b.wg.Wait()
}

// Flush вызывает batchExecutor, если буфер SQL запросов заполнен не менее, чем на maxBufLen
func (b *PgBatchRowQuery) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.flush(0)
}

// Flush вызывает batchExecutor, если буфер SQL запросов заполнен не менее, чем на maxBufLen
func (b *PgBatchRowQuery) flush(maxBufLen int) {
	for sID, shrd := range b.shards {
		b.launchQuery(sID, shrd, maxBufLen)
	}
}

func (b *PgBatchRowQuery) launchQuery(sID string, shrd *bShard, limit int) {
	if limit <= 0 {
		limit = 1
	}
	if len(shrd.brQueries) >= limit {
		b.wg.Add(1)
		go b.query(shrd)
		b.shards[sID] = newShard(shrd.pg)
		b.lastFlush.Store(time.Now().UnixNano()) // Обновляем таймер
	}
}

func newShard(pg pgc.PgInstance) *bShard {
	return &bShard{
		pg:        pg,
		brQueries: make([]*BRQuery, 0, BatchBufSize),
	}
}

func (b *PgBatchRowQuery) query(shrd *bShard) {
	defer b.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			slog.Error(
				"panic recovered",
				slog.Any("reason", r),
			)
		}
	}()

	// Подготавляваем батч
	batch := &pgx.Batch{}
	for i := range shrd.brQueries {
		batch.Queue(shrd.brQueries[i].SQL, shrd.brQueries[i].Params...)
	}

	// Кладем токен и блокируемся в очереди
	b.chBatchLimiter <- true
	defer func() { <-b.chBatchLimiter }()

	// Вызываем исполнятор батча в транзакции
	execCtx := context.WithoutCancel(b.ctx)
	err := shrd.pg.Tx(execCtx, func(ctx context.Context, tx pgc.PgxTxIface) error {
		br := tx.SendBatch(ctx, batch)
		defer br.Close()

		for i := range shrd.brQueries {
			err := br.QueryRow().Scan(shrd.brQueries[i].Results...)
			if err != nil {
				return err
			}
			shrd.brQueries[i].Err = nil
		}

		return nil
	})

	if err != nil {
		slog.Error("batch exec fail",
			slog.Any("err", err),
		)
		for i := range shrd.brQueries {
			shrd.brQueries[i].Err = err
		}
	}

	// Возвращаем результат
	for i := range shrd.brQueries {
		select {
		case b.chOut <- shrd.brQueries[i]:
		case <-time.After(5 * time.Second):
			slog.Error("result delivery timeout, consumer is not reading",
				slog.Any("BRQuery", shrd.brQueries[i]),
			)
			return
		}
	}
}
