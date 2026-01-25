package pgc

import (
	"context"
	"testing"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ManualMock реализует IPgxPool
type ManualMock struct {
	PingErr    error
	BeginErr   error
	Delay      time.Duration
	MockConfig *pgxpool.Config
}

func (m *ManualMock) Ping(ctx context.Context) error {
	time.Sleep(m.Delay)
	return m.PingErr
}

func (m *ManualMock) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, m.BeginErr
}

func (m *ManualMock) Close() {}

func (m *ManualMock) Config() *pgxpool.Config {
	if m.MockConfig == nil {
		// Минимальный конфиг, чтобы Host() не падал
		config, _ := pgxpool.ParseConfig("postgres://localhost:5432/db")
		return config
	}
	return m.MockConfig
}

func TestPgHndl_Ping_Latency(t *testing.T) {
	mock := &ManualMock{
		Delay: 50 * time.Millisecond,
	}

	h := &PgHndl{
		name:   "test-db",
		pgPool: mock,
	}

	t.Run("Check Latency Calculation", func(t *testing.T) {
		err := h.Ping(context.Background())
		assert.NoError(t, err)

		// Проверяем, что латентность замерилась корректно (>= 50ms)
		assert.GreaterOrEqual(t, h.Latency().Milliseconds(), int64(50))
	})

	t.Run("Offline on Network Error", func(t *testing.T) {
		// Сбрасываем кэш (3 сек)
		h.mu.Lock()
		h.lastCheckTime = time.Now().Add(-5 * time.Second)
		h.mu.Unlock()

		mock.PingErr = &pgconn.PgError{Code: "08000"} // Сетевая ошибка

		err := h.Ping(context.Background())
		assert.Error(t, err)
		assert.False(t, h.IsReady())
	})
}

func TestPgHndl_Begin_PanicRecovery(t *testing.T) {
	// Для тестирования Begin с реальным транзакционным поведением
	// лучше использовать pgxmock, но Panic Recovery проверяется так:
	h := &PgHndl{
		name: "panic-test",
		// Нам нужен мок, который упадет при вызове Begin
		pgPool: &ManualMock{BeginErr: nil},
	}
	h.Online()

	err := h.Begin(context.Background(), func(tx pgx.Tx) error {
		panic("boom")
	})

	assert.ErrorIs(t, err, ErrPanicRecovered)
}

// Тестируем Unit of Work (Begin) и Panic Recovery
func TestPgHndl_Begin(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	h := &PgHndl{name: "db", pgPool: mock}
	h.Online()
	ctx := context.Background()

	t.Run("Success Transaction", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectCommit()

		err := h.Begin(ctx, func(tx pgx.Tx) error {
			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("Panic Recovery Test", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectRollback() // Должен вызваться автоматически

		err := h.Begin(ctx, func(tx pgx.Tx) error {
			panic("critical failure")
		})

		assert.ErrorIs(t, err, ErrPanicRecovered)
	})
}
