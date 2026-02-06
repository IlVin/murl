package repository

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Mock конфигурации
type mockCfg struct {
	drv    string
	shards byte
}

func (m mockCfg) RepoDrv() string { return m.drv }
func (m mockCfg) ShardSize() byte { return m.shards }

func TestNewRepoDrv(t *testing.T) {
	t.Run("Create InMemory", func(t *testing.T) {
		cfg := mockCfg{drv: "InMemory", shards: 2}
		repo := NewRepoDrv(cfg)
		assert.NotNil(t, repo)
		assert.IsType(t, &InMemoryRepoDrv{}, repo)
	})

	t.Run("Create PgDB", func(t *testing.T) {
		cfg := mockCfg{drv: "PgDB", shards: 2}
		repo := NewRepoDrv(cfg)
		assert.NotNil(t, repo)
	})

	t.Run("Unknown driver panic", func(t *testing.T) {
		cfg := mockCfg{drv: "Redis", shards: 1}
		assert.Panics(t, func() {
			NewRepoDrv(cfg)
		})
	})
}

func TestInMemoryRepoDrv_UpSert(t *testing.T) {
	repo := NewRepoDrv(mockCfg{drv: "InMemory", shards: 2})

	t.Run("Successful UpSert", func(t *testing.T) {
		idx, err := repo.UpSert(0, "url1")
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), idx)

		// Повторный UpSert того же значения (проверка ветки RLock)
		idx2, err := repo.UpSert(0, "url1")
		assert.NoError(t, err)
		assert.Equal(t, idx, idx2)
	})

	t.Run("Shard out of bounds", func(t *testing.T) {
		_, err := repo.UpSert(5, "url")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "out of range")
	})

	t.Run("Concurrent UpSert (Race Condition check)", func(t *testing.T) {
		var wg sync.WaitGroup
		const iterations = 100
		for i := 0; i < iterations; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				// Пишем одну и ту же строку многократно из разных потоков
				_, _ = repo.UpSert(1, "duplicate")
			}(i)
		}
		wg.Wait()

		val, _ := repo.Select(1, 0)
		assert.Equal(t, "duplicate", val)
	})
}

func TestInMemoryRepoDrv_Select(t *testing.T) {
	repo := NewRepoDrv(mockCfg{drv: "InMemory", shards: 1})
	_, _ = repo.UpSert(0, "find-me")

	t.Run("Found", func(t *testing.T) {
		val, err := repo.Select(0, 0)
		assert.NoError(t, err)
		assert.Equal(t, "find-me", val)
	})

	t.Run("Not Found", func(t *testing.T) {
		_, err := repo.Select(0, 999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "record not found")
	})

	t.Run("Shard out of bounds", func(t *testing.T) {
		_, err := repo.Select(2, 0)
		assert.Error(t, err)
	})
}

func TestInMemoryRepoDrv_Set(t *testing.T) {
	repo := NewRepoDrv(mockCfg{drv: "InMemory", shards: 1})

	t.Run("Set at the end (append)", func(t *testing.T) {
		err := repo.Set(0, 0, "first")
		assert.NoError(t, err)
		val, _ := repo.Select(0, 0)
		assert.Equal(t, "first", val)
	})

	t.Run("Set with gap (index > len)", func(t *testing.T) {
		// Текущая длина 1. Ставим на индекс 3.
		// Должно создаться 2 пустых строки (падинг) + наша строка.
		err := repo.Set(0, 3, "gap-fill")
		assert.NoError(t, err)

		val, _ := repo.Select(0, 3)
		assert.Equal(t, "gap-fill", val)

		empty, _ := repo.Select(0, 1)
		assert.Equal(t, "", empty)
	})

	t.Run("Overwrite existing", func(t *testing.T) {
		err := repo.Set(0, 0, "overwritten")
		assert.NoError(t, err)
		val, _ := repo.Select(0, 0)
		assert.Equal(t, "overwritten", val)
	})

	t.Run("Shard out of bounds", func(t *testing.T) {
		err := repo.Set(5, 0, "fail")
		assert.Error(t, err)
	})
}
