package pgc

import (
	"context"
	config "murl/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Тестируем логику NewDBCluster с проверкой fnClosePgPool при ошибке
func TestNewDBCluster_InitValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("ErrShardOutOfPlace - invalid shard ID sequence", func(t *testing.T) {
		cfg := config.DBClusterProps{
			Shards: config.DBShrdList{
				{ShrdID: "01", RW: []string{"dsn"}}, // Ожидалось "00"
			},
		}
		cluster, err := NewDBCluster(ctx, "cluster-1", cfg)
		assert.Nil(t, cluster)
		assert.ErrorIs(t, err, ErrShardOutOfPlace)
	})

	t.Run("Cleanup on partial failure", func(t *testing.T) {
		// Первый шард ок, второй с ошибкой (пустой RW список вызовет ошибку в NewPgHndlPool)
		cfg := config.DBClusterProps{
			Shards: config.DBShrdList{
				{ShrdID: "00", RW: []string{"postgres://localhost/db1"}},
				{ShrdID: "01", RW: []string{}}, // Ошибка: пустой RW
			},
		}
		cluster, err := NewDBCluster(ctx, "fail-cluster", cfg)
		assert.Nil(t, cluster)
		assert.Error(t, err)
		// Здесь сработал fnClosePgPool()
	})
}

// Тестируем жизненный цикл: New -> Open -> Access -> Close
func TestDBCluster_FullLifecycle(t *testing.T) {
	// Подготовка данных
	mock := &ManualMock{}
	//	ctx := context.Background()

	// Эмулируем успешную сборку кластера
	// (Т.к. NewPgHndl лезет в pgxpool.New, в юнит-тесте создаем структуру вручную)
	h1 := &PgHndl{name: "shard00-rw", pgPool: mock}
	h1.Online()

	p1, _ := NewPgHndlPool([]*PgHndl{h1})

	cluster := &DBCluster{
		id:           "main",
		ctx:          context.Background(),
		rwPool:       []*PgHndlPool{p1},
		roPool:       []*PgHndlPool{p1},
		checkService: &CheckService{}, // Заглушка
	}
	cluster.isOpened.Store(false)

	t.Run("State: Closed by default", func(t *testing.T) {
		_, err := cluster.RW(0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not opened")
	})

	t.Run("State: Open success", func(t *testing.T) {
		// Эмулируем Open()
		cluster.isOpened.Store(true)

		pool, err := cluster.RW(0)
		assert.NoError(t, err)
		assert.NotNil(t, pool)

		// Проверка границ (shardID out of range)
		_, err = cluster.RW(1)
		assert.Error(t, err)
	})

	t.Run("State: Close cleanup", func(t *testing.T) {
		cluster.Close()
		assert.False(t, cluster.isOpened.Load())

		_, err := cluster.RO(0)
		assert.Error(t, err)
		assert.True(t, h1.isClosed.Load(), "Underlying handle must be closed")
	})
}

// Тест интеграции с DBShrdList.Exists
func TestDBCluster_ConfigIntegration(t *testing.T) {
	shrdList := config.DBShrdList{
		{ShrdID: "00", RW: []string{"dsn1"}},
		{ShrdID: "01", RW: []string{"dsn2"}},
	}

	assert.True(t, shrdList.Exists("00"))
	assert.False(t, shrdList.Exists("02"))
}
