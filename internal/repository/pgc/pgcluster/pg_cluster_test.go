package pgcluster

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"murl/internal/repository/pgc"
)

// MockConfig для тестов
type mockConfig struct {
	shardSize byte
	dsn       string
}

func (m *mockConfig) ShardSize() byte { return m.shardSize }
func (m *mockConfig) DBDSN() string   { return m.dsn }

func TestPgCluster_New_Validation(t *testing.T) {
	ctx := context.Background()

	// Тест на некорректный размер шарда
	cfg := &mockConfig{shardSize: 0}
	cluster, err := NewPgCluster(ctx, cfg, nil)
	assert.Error(t, err)
	assert.Nil(t, cluster)
}

func TestPgCluster_ShardingLogic(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInst := NewMockPgInstance(ctrl)
	shardSize := byte(5)

	// Создаем кластер вручную, чтобы подсунуть мок инстанса
	c := &pgCluster{
		instances: []pgc.PgInstance{mockInst},
		shards:    make([]pgc.PgInstance, shardSize),
	}
	for i := range c.shards {
		c.shards[i] = mockInst
	}

	// 1. Проверка размера
	assert.Equal(t, shardSize, c.Size())

	// 2. Проверка GetShard
	shard, err := c.GetShard(2)
	assert.NoError(t, err)
	assert.Equal(t, mockInst, shard)

	// 3. Проверка выхода за границы
	_, err = c.GetShard(10)
	assert.Error(t, err)
}

func TestPgCluster_Ping_StopOnFirstError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Эмулируем два разных инстанса (хотя в текущей имплементации он один)
	mockInst1 := NewMockPgInstance(ctrl)
	mockInst2 := NewMockPgInstance(ctrl)

	c := &pgCluster{
		instances: []pgc.PgInstance{mockInst1, mockInst2},
	}

	ctx := context.Background()
	testErr := errors.New("ping failed")

	// Ожидаем, что если первый упал, второй даже не будут пинговать
	mockInst1.EXPECT().Ping(ctx).Return(testErr)

	err := c.Ping(ctx)
	assert.ErrorIs(t, err, testErr)
}

func TestPgCluster_Close_AllInstances(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInst1 := NewMockPgInstance(ctrl)
	mockInst2 := NewMockPgInstance(ctrl)

	c := &pgCluster{
		instances: []pgc.PgInstance{mockInst1, mockInst2},
	}

	// Ожидаем вызов Close на ВСЕХ уникальных инстансах
	mockInst1.EXPECT().Close().Return(nil)
	mockInst2.EXPECT().Close().Return(nil)

	err := c.Close()
	assert.NoError(t, err)
}

func TestPgCluster_RunMigrations_ContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInst := NewMockPgInstance(ctrl)
	mockInst.EXPECT().String().Return("localhost:5432").AnyTimes()

	c := &pgCluster{
		instances: []pgc.PgInstance{mockInst},
	}

	// Отменяем контекст сразу
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.RunMigrations(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}
