package repository

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"

	"murl/internal/model/event"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestInMemoryRepoDrv(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Настройка конфига: 2 шарда
	mockCfg := NewMockRepoDrvConfig(ctrl)
	mockCfg.EXPECT().ShardSize().Return(uint8(2)).AnyTimes()

	ctx := context.Background()

	t.Run("UpSert and Select", func(t *testing.T) {
		drv := newInMemoryRepoDrv(mockCfg)
		url := "https://example.com"
		shardID := byte(0)

		// Первая запись
		idx, cf, err := drv.UpSert(ctx, shardID, url)
		assert.NoError(t, err)
		assert.False(t, cf)
		assert.Equal(t, uint64(1), idx)

		// Повторная запись того же URL (Conflict)
		idx2, cf2, err := drv.UpSert(ctx, shardID, url)
		assert.NoError(t, err)
		assert.True(t, cf2)
		assert.Equal(t, idx, idx2)

		// Проверка через Select
		res, err := drv.Select(ctx, shardID, idx)
		assert.NoError(t, err)
		assert.Equal(t, url, res)
	})

	t.Run("Sharding isolation", func(t *testing.T) {
		drv := newInMemoryRepoDrv(mockCfg)
		url := "https://unique.com"
		// Пишем в разные шарды
		idx0, _, _ := drv.UpSert(ctx, 0, url)
		idx1, _, _ := drv.UpSert(ctx, 1, url)

		// В каждом шарде свой счетчик lastIdx, поэтому индексы могут совпасть (оба 1)
		// Но данные лежат в разных структурах
		assert.Equal(t, idx0, idx1)

		res0, _ := drv.Select(ctx, 0, idx0)
		res1, _ := drv.Select(ctx, 1, idx1)
		assert.Equal(t, url, res0)
		assert.Equal(t, url, res1)
	})

	t.Run("BatchUpSert", func(t *testing.T) {
		drv := newInMemoryRepoDrv(mockCfg)
		batch := []event.PayloadBatchItem{
			{OrigURL: "b1", ShardID: 0},
			{OrigURL: "b2", ShardID: 1},
		}

		res, err := drv.BatchUpSert(ctx, batch)
		assert.NoError(t, err)
		assert.Len(t, res, 2)
		assert.NotEqual(t, uint64(0), res[1].Idx) // Проверка, что во втором шарде тоже прошла запись
	})

	t.Run("Set logic", func(t *testing.T) {
		drv := newInMemoryRepoDrv(mockCfg)
		shardID := byte(0)
		customIdx := uint64(100)
		url := "https://custom.com"

		err := drv.Set(ctx, shardID, customIdx, url)
		assert.NoError(t, err)

		// Проверяем, что индекс обновился
		res, _ := drv.Select(ctx, shardID, customIdx)
		assert.Equal(t, url, res)

		// Проверяем, что lastIdx сдвинулся
		newIdx, _, _ := drv.UpSert(ctx, shardID, "next-url")
		assert.Equal(t, uint64(101), newIdx)
	})

	t.Run("Errors and Limits", func(t *testing.T) {
		drv := newInMemoryRepoDrv(mockCfg)
		// Ошибка шарда
		_, _, err := drv.UpSert(ctx, 10, "url")
		assert.Error(t, err)

		// Запись несуществующего индекса
		_, err = drv.Select(ctx, 0, 9999)
		assert.Error(t, err)

		// Overflow check (lastIdx)
		shard, _ := (drv.(*InMemoryRepoDrv)).getShard(0)
		shard.lastIdx = math.MaxUint64
		_, _, err = drv.UpSert(ctx, 0, "any")
		assert.EqualError(t, err, "lastIdx overflow")
	})

	t.Run("Concurrent access", func(t *testing.T) {
		drv := newInMemoryRepoDrv(mockCfg)
		const goroutines = 100
		const ops = 100
		wg := sync.WaitGroup{}
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < ops; j++ {
					u := fmt.Sprintf("url-%d-%d", id, j)
					_, _, _ = drv.UpSert(ctx, 0, u)
				}
			}(i)
		}
		wg.Wait()
		// Если race detector (go test -race) молчит, тест пройден
	})

	t.Run("Ping and Migrations", func(t *testing.T) {
		drv := newInMemoryRepoDrv(mockCfg)
		assert.NoError(t, drv.Ping(ctx))
		assert.NoError(t, drv.RunMigrations(ctx))
	})
}
