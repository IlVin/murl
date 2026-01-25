package pgc

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockPool реализует IPgHndlPool
type MockPool struct {
	CheckFn   func(ctx context.Context) error
	SizeFn    func() int
	OfflineFn func()
	PoolStats PoolStats
}

func (m *MockPool) Begin(ctx context.Context, cb func(pgx.Tx) error) error { return nil }
func (m *MockPool) Check(ctx context.Context) error {
	if m.CheckFn != nil {
		return m.CheckFn(ctx)
	}
	return nil
}
func (m *MockPool) Offline() {
	if m.OfflineFn != nil {
		m.OfflineFn()
	}
}
func (m *MockPool) Size() int {
	if m.SizeFn != nil {
		return m.SizeFn()
	}
	return 1
}
func (m *MockPool) Stats() PoolStats {
	return m.PoolStats
}

// Тест конструктора и группировки
func TestNewCheckService(t *testing.T) {
	mockConfigA := &ManualMock{}
	configA, _ := pgxpool.ParseConfig("postgres://user:pass@host_a:5432/db")
	mockConfigA.MockConfig = configA

	mockConfigB := &ManualMock{}
	configB, _ := pgxpool.ParseConfig("postgres://user:pass@host_b:5432/db")
	mockConfigB.MockConfig = configB

	// 2 хендла на хост A, 1 на хост B
	h1 := &PgHndl{name: "h1-a", pgPool: mockConfigA}
	h2 := &PgHndl{name: "h2-a", pgPool: mockConfigA}
	h3 := &PgHndl{name: "h3-b", pgPool: mockConfigB}

	service, err := NewCheckService([]*PgHndl{h1, h2, h3})
	require.NoError(t, err)

	assert.Len(t, service.instances, 2, "Should have 2 unique instances")
	assert.Equal(t, 2, service.instances["host_a"].Size())
	assert.Equal(t, 1, service.instances["host_b"].Size())
}

// Тест жизненного цикла: Старт -> Стоп -> Старт -> Стоп
func TestCheckService_StartStop_Sequence(t *testing.T) {
	mockConfig := &ManualMock{}
	config, _ := pgxpool.ParseConfig("postgres://l:5432/db")
	mockConfig.MockConfig = config
	h1 := &PgHndl{name: "h1", pgPool: mockConfig}
	service, _ := NewCheckService([]*PgHndl{h1})

	ctx := context.Background()

	t.Run("Start once", func(t *testing.T) {
		err := service.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, service.isRunning.Load())
	})

	t.Run("Start again returns error", func(t *testing.T) {
		err := service.Start(ctx)
		assert.ErrorIs(t, err, ErrAlreadyRunning)
	})

	t.Run("Stop works", func(t *testing.T) {
		err := service.Stop()
		assert.NoError(t, err)
		assert.False(t, service.isRunning.Load())
	})

	t.Run("Stop again returns error", func(t *testing.T) {
		err := service.Stop()
		assert.ErrorIs(t, err, ErrAlreadyStop)
	})
}

// Тест Graceful Shutdown
func TestCheckService_GracefulShutdown(t *testing.T) {
	mockConfig := &ManualMock{}
	config, _ := pgxpool.ParseConfig("postgres://l:5432/db")
	mockConfig.MockConfig = config
	h1 := &PgHndl{name: "h1", pgPool: mockConfig}
	service, _ := NewCheckService([]*PgHndl{h1})

	// Запускаем в фоновом режиме
	service.Start(context.Background())

	// Stop должен дождаться завершения горутины воркера
	stopChan := make(chan struct{})
	go func() {
		service.Stop()
		close(stopChan)
	}()

	select {
	case <-stopChan:
		// Успешно завершилось
	case <-time.After(time.Second):
		t.Fatal("Stop() did not finish within a second, likely a goroutine leak or sync issue")
	}
}
