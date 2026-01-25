package pgc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

func TestPgHndlPool_New(t *testing.T) {
	t.Run("Empty handles returns error", func(t *testing.T) {
		p, err := NewPgHndlPool([]*PgHndl{})
		assert.Nil(t, p)
		assert.ErrorIs(t, err, ErrIsEmpty)
	})
}

func TestPgHndlPool_RoundRobin_Begin(t *testing.T) {
	mock := &ManualMock{}
	h1 := &PgHndl{name: "h1", pgPool: mock}
	h2 := &PgHndl{name: "h2", pgPool: mock}
	h1.Online()
	h2.Online()

	pool, _ := NewPgHndlPool([]*PgHndl{h1, h2})
	ctx := context.Background()

	// Проверяем, что Begin действительно перебирает разные хендлы (Round-Robin)
	// Для этого мы можем просто вызвать Begin многократно параллельно
	var wg sync.WaitGroup
	iterations := 100
	wg.Add(iterations)

	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			_ = pool.Begin(ctx, func(tx pgx.Tx) error { return nil })
		}()
	}
	wg.Wait()

	// После 100 итераций на 2 хендла, счетчик p.curr должен вырасти на 100
	assert.Equal(t, uint32(iterations), pool.curr)
}

func TestPgHndlPool_Check_Logic(t *testing.T) {
	mock := &ManualMock{}
	h1 := &PgHndl{name: "h1", pgPool: mock}
	h2 := &PgHndl{name: "h2", pgPool: mock}

	pool, _ := NewPgHndlPool([]*PgHndl{h1, h2})
	ctx := context.Background()

	t.Run("Priority checking offline handles", func(t *testing.T) {
		h1.Online()
		h2.Offline()

		// Сбрасываем кэш пинга у h2
		h2.mu.Lock()
		h2.lastCheckTime = time.Now().Add(-10 * time.Second)
		h2.mu.Unlock()

		// Метод Check должен найти h2 (так как он Offline) и вызвать Ping у него
		err := pool.Check(ctx)
		assert.NoError(t, err)
		assert.True(t, h2.IsReady(), "h2 should become online after Check")
	})

	t.Run("Preventive ping for online handles", func(t *testing.T) {
		h1.Online()
		h2.Online()

		// Если все Online, Check должен просто пинговать следующий по счетчику
		startVal := pool.curr
		err := pool.Check(ctx)
		assert.NoError(t, err)
		assert.Equal(t, startVal+1, pool.curr, "Counter should increment")
	})
}

func TestPgHndlPool_Stats(t *testing.T) {
	mock := &ManualMock{}
	h1 := &PgHndl{name: "h1", pgPool: mock}
	h2 := &PgHndl{name: "h2", pgPool: mock}

	// Настраиваем разные задержки
	h1.Online()
	h1.mu.Lock()
	h1.lastLatency = 10 * time.Millisecond
	h1.mu.Unlock()

	h2.Online()
	h2.mu.Lock()
	h2.lastLatency = 30 * time.Millisecond
	h2.mu.Unlock()

	pool, _ := NewPgHndlPool([]*PgHndl{h1, h2})

	t.Run("Correct aggregate stats", func(t *testing.T) {
		stats := pool.Stats()

		assert.Equal(t, 2, stats.Total)
		assert.Equal(t, 2, stats.Online)
		assert.Equal(t, 0, stats.Offline)
		assert.Equal(t, 30*time.Millisecond, stats.MaxLatency)
		assert.Equal(t, 20*time.Millisecond, stats.AvgLatency)
		assert.Equal(t, int64(20), stats.AvgLatencyMs)
	})

	t.Run("Stats with failed instances", func(t *testing.T) {
		h2.Offline()
		stats := pool.Stats()

		assert.Equal(t, 1, stats.Online)
		assert.Equal(t, 1, stats.Offline)
		assert.Contains(t, stats.Failed, "h2")
		// Среднее теперь только по h1
		assert.Equal(t, 10*time.Millisecond, stats.AvgLatency)
	})
}

func TestPgHndlPool_Offline_All(t *testing.T) {
	// Создаем мок, чтобы Host() не падал
	mock := &ManualMock{}

	h1 := &PgHndl{name: "h1", pgPool: mock}
	h2 := &PgHndl{name: "h2", pgPool: mock}

	h1.Online()
	h2.Online()

	pool, _ := NewPgHndlPool([]*PgHndl{h1, h2})
	pool.Offline()

	assert.False(t, h1.IsReady())
	assert.False(t, h2.IsReady())
}

func TestPgHndlPool_NoReadyConnections(t *testing.T) {
	mock := &ManualMock{PingErr: errors.New("conn error")}
	h1 := &PgHndl{name: "h1", pgPool: mock}
	h1.Offline()

	pool, _ := NewPgHndlPool([]*PgHndl{h1})

	// Сбрасываем кэш пинга, чтобы Begin инициировал реальный Ping
	h1.mu.Lock()
	h1.lastCheckTime = time.Now().Add(-10 * time.Second)
	h1.mu.Unlock()

	err := pool.Begin(context.Background(), func(tx pgx.Tx) error { return nil })
	assert.ErrorIs(t, err, ErrNoReadyConnections)
}
