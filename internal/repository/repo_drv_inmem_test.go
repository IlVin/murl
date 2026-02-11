package repository

import (
	"context"
	"fmt"
	"math"
	"murl/internal/model/event"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockConfig для тестов
type mockConfig struct {
	shardSize byte
}

func (m mockConfig) ShardSize() byte          { return m.shardSize }
func (m mockConfig) DBDSN() string            { return "" }
func (m mockConfig) RepoDrv() string          { return "InMemory" }
func (m mockConfig) EventStoragePath() string { return "" }

func TestInMemory_UpSert(t *testing.T) {
	ctx := context.Background()
	cfg := mockConfig{shardSize: 2}
	repo := newInMemoryRepoDrv(cfg)

	t.Run("new record creation", func(t *testing.T) {
		id, err := repo.UpSert(ctx, 0, "http://google.com")
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), id)

		// Повторный UpSert того же URL должен вернуть тот же ID
		id2, err := repo.UpSert(ctx, 0, "http://google.com")
		assert.NoError(t, err)
		assert.Equal(t, id, id2)
	})

	t.Run("different shards", func(t *testing.T) {
		// Тот же URL в другом шарде — это новая запись (нормально для шардирования)
		id, err := repo.UpSert(ctx, 1, "http://google.com")
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), id) // В этом шарде индекс начался с 0
	})
}

func TestInMemory_SetAndSelect(t *testing.T) {
	ctx := context.Background()
	cfg := mockConfig{shardSize: 1}
	repo := newInMemoryRepoDrv(cfg)

	t.Run("set then select", func(t *testing.T) {
		err := repo.Set(ctx, 0, 100, "http://apple.com")
		assert.NoError(t, err)

		val, err := repo.Select(ctx, 0, 100)
		assert.NoError(t, err)
		assert.Equal(t, "http://apple.com", val)
	})

	t.Run("overwrite existing index", func(t *testing.T) {
		// URL "A" -> ID 1
		_ = repo.Set(ctx, 0, 1, "A")
		// URL "B" -> ID 2
		_ = repo.Set(ctx, 0, 2, "B")

		// Перезаписываем ID 1 новым URL "B"
		// Старая связь B->2 должна удалиться, B->1 появиться, A->1 исчезнуть.
		err := repo.Set(ctx, 0, 1, "B")
		assert.NoError(t, err)

		// Проверяем, что ID 2 больше не содержит "B"
		_, err = repo.Select(ctx, 0, 2)
		assert.Error(t, err, "ID 2 should have been cleared because URL 'B' moved to ID 1")

		// Проверяем, что ID 1 теперь "B"
		val, _ := repo.Select(ctx, 0, 1)
		assert.Equal(t, "B", val)
	})
}

func TestInMemory_BatchUpSert(t *testing.T) {
	ctx := context.Background()
	cfg := mockConfig{shardSize: 2}
	repo := newInMemoryRepoDrv(cfg)

	batch := []event.PayloadBatchItem{
		{CorrelationID: "c1", OrigURL: "u1", ShardID: 0}, // Получит Shard 0, Idx 0
		{CorrelationID: "c2", OrigURL: "u2", ShardID: 1}, // Получит Shard 1, Idx 0
		{CorrelationID: "c3", OrigURL: "u1", ShardID: 0}, // Получит Shard 0, Idx 0 (дубликат)
		{CorrelationID: "c4", OrigURL: "u3", ShardID: 0}, // Получит Shard 0, Idx 1
	}

	res, err := repo.BatchUpSert(ctx, batch)
	assert.NoError(t, err)
	require.Len(t, res, 4)

	// 1. Проверяем, что дубликат URL в одном шарде вернул тот же ID
	assert.Equal(t, res[0].Idx, res[2].Idx, "u1 in shard 0 should have same ID")

	// 2. Проверяем, что разные URL в одном шарде имеют разные ID
	assert.NotEqual(t, res[0].Idx, res[3].Idx, "u1 and u3 in shard 0 must have different IDs")

	// 3. Проверяем, что данные записаны в правильные шарды
	assert.Equal(t, byte(0), res[0].ShardID)
	assert.Equal(t, byte(1), res[1].ShardID)
}

func TestInMemory_Errors(t *testing.T) {
	ctx := context.Background()
	repo := newInMemoryRepoDrv(mockConfig{shardSize: 1})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.Select(ctx, 0, 999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "record not found")
	})

	t.Run("shard out of range", func(t *testing.T) {
		err := repo.Set(ctx, 5, 1, "test")
		assert.Error(t, err)
	})

	t.Run("max index error", func(t *testing.T) {
		err := repo.Set(ctx, 0, math.MaxUint64, "test")
		assert.Error(t, err)
		assert.Equal(t, "cannot set max uint64 index", err.Error())
	})
}

// Тест на конкурентность (Race Detector)
func TestInMemory_Race(t *testing.T) {
	ctx := context.Background()
	cfg := mockConfig{shardSize: 4}
	repo := newInMemoryRepoDrv(cfg)

	wg := sync.WaitGroup{}
	workers := 20
	iterations := 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				shard := byte(workerID % 4)
				url := fmt.Sprintf("url-%d-%d", workerID, j)

				// Одновременно читаем и пишем
				_, _ = repo.UpSert(ctx, shard, url)
				_, _ = repo.Select(ctx, shard, uint64(j))
				_ = repo.Set(ctx, shard, uint64(j+1000), url)
			}
		}(i)
	}

	wg.Wait()
}
