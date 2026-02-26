package pgcluster

import (
	"context"
	"errors"
	"murl/internal/mocks"
	"murl/internal/repository/pgc/instance"
	"murl/internal/repository/pgc/metrics"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewPgCluster_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCfg := mocks.NewMockPgClusterConfig(ctrl)
	reg := prometheus.NewRegistry()
	m, _ := metrics.NewPgMetrics(reg)
	t.Run("zero_shard_size", func(t *testing.T) {
		mockCfg.EXPECT().ShardSize().Return(byte(0))
		cluster, err := NewPgCluster(context.Background(), mockCfg, m)
		assert.Error(t, err)
		assert.Nil(t, cluster)
		assert.Contains(t, err.Error(), "greater than 0")
	})
	t.Run("invalid_dsn", func(t *testing.T) {
		mockCfg.EXPECT().ShardSize().Return(byte(1))
		mockCfg.EXPECT().DBDSN().Return("invalid-connection-string")
		cluster, err := NewPgCluster(context.Background(), mockCfg, m)
		assert.Error(t, err)
		assert.Nil(t, cluster)
	})
}
func TestPgCluster_Sharding(t *testing.T) {
	// Создаем структуру вручную для тестирования логики без коннекта к БД
	c := &pgCluster{
		shards: make([]*instance.PgInstance, 10),
	}
	t.Run("Size", func(t *testing.T) {
		assert.Equal(t, byte(10), c.Size())
	})
	t.Run("GetShard_Success", func(t *testing.T) {
		shard, err := c.GetShard(5)
		assert.NoError(t, err)
		assert.Nil(t, shard) // Т.к. мы просто аллоцировали слайс nil-ов
	})
	t.Run("GetShard_OutOfRange", func(t *testing.T) {
		shard, err := c.GetShard(10)
		assert.Error(t, err)
		assert.Nil(t, shard)
		assert.Contains(t, err.Error(), "out of range")
	})
	t.Run("ShardID_Consistency", func(t *testing.T) {
		key := "test-url"
		id1 := c.ShardID(key)
		id2 := c.ShardID(key)
		assert.Equal(t, id1, id2)
		assert.True(t, id1 < 10)
	})
}
func TestPgCluster_Lifecycle_Mock(t *testing.T) {
	// Проверка поведения при пустых инстансах
	c := &pgCluster{
		instances: []*instance.PgInstance{nil},
	}
	t.Run("Close_With_Nil", func(t *testing.T) {
		err := c.Close()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil instance")
	})
	t.Run("RunMigrations_ContextCancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := c.RunMigrations(ctx)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	})
}
