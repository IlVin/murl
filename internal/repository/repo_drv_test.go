package repository

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// mockDrvConfig реализует интерфейс RepoDrvConfig
type mockDrvConfig struct {
	drv       string
	shardSize byte
	logger    *zap.Logger
}

func (m mockDrvConfig) RepoDrv() string { return m.drv }
func (m mockDrvConfig) ShardSize() byte { return m.shardSize }
func (m mockDrvConfig) Zap() *zap.Logger {
	if m.logger == nil {
		return zap.NewNop()
	}
	return m.logger
}

func TestNewRepoDrv(t *testing.T) {
	logger := zap.NewNop()

	t.Run("InMemory driver", func(t *testing.T) {
		cfg := mockDrvConfig{drv: "InMemory", shardSize: 10, logger: logger}
		drv := NewRepoDrv(cfg)
		assert.IsType(t, &InMemoryRepoDrv{}, drv)
	})

	t.Run("PgDB driver stub", func(t *testing.T) {
		cfg := mockDrvConfig{drv: "PgDB", shardSize: 10, logger: logger}
		drv := NewRepoDrv(cfg)
		assert.IsType(t, &InMemoryRepoDrv{}, drv)
	})

	t.Run("Unknown driver panics", func(t *testing.T) {
		cfg := mockDrvConfig{drv: "Unknown", shardSize: 10, logger: logger}

		// Проверяем, что функция вызывает панику
		assert.PanicsWithValue(t, "Unknow repo driver", func() {
			NewRepoDrv(cfg)
		}, "Код должен паниковать при неизвестном драйвере")
	})
}

func TestInMemoryRepoDrv_UpSert(t *testing.T) {
	logger := zap.NewNop()
	cfg := mockDrvConfig{drv: "InMemory", shardSize: 2, logger: logger}
	drv := NewRepoDrv(cfg)

	t.Run("Successful first insert", func(t *testing.T) {
		idx, err := drv.UpSert(0, "test-1")
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), idx)
	})

	t.Run("Existing string (hit RLock)", func(t *testing.T) {
		idx, err := drv.UpSert(0, "test-1")
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), idx)
	})

	t.Run("Shard ID out of range (check error log branch)", func(t *testing.T) {
		_, err := drv.UpSert(5, "fail")
		assert.ErrorIs(t, err, ErrOutOfRange)
	})

	t.Run("Race condition double-check", func(t *testing.T) {
		const goroutines = 50
		const testStr = "race-string"
		var wg sync.WaitGroup

		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				_, _ = drv.UpSert(1, testStr)
			}()
		}
		wg.Wait()

		idx, _ := drv.UpSert(1, testStr)
		assert.Equal(t, uint64(0), idx)
	})
}

func TestInMemoryRepoDrv_Select(t *testing.T) {
	logger := zap.NewNop()
	cfg := mockDrvConfig{drv: "InMemory", shardSize: 1, logger: logger}
	drv := NewRepoDrv(cfg)

	testStr := "select-me"
	idx, _ := drv.UpSert(0, testStr)

	t.Run("Successful select", func(t *testing.T) {
		val, err := drv.Select(0, idx)
		assert.NoError(t, err)
		assert.Equal(t, testStr, val)
	})

	t.Run("Record not found", func(t *testing.T) {
		_, err := drv.Select(0, 999)
		assert.ErrorIs(t, err, ErrDBRecordNotFound)
	})

	t.Run("Shard ID out of range", func(t *testing.T) {
		_, err := drv.Select(10, 0)
		assert.ErrorIs(t, err, ErrOutOfRange)
	})
}
