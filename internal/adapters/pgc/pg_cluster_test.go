package pgc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPgCluster_Integration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// 1. Создаем моки физических соединений
	mockPhys1 := NewMockPgInstance(ctrl)
	mockPhys2 := NewMockPgInstance(ctrl)

	mockPhys1.EXPECT().String().Return("host1:5432/db").AnyTimes()
	mockPhys2.EXPECT().String().Return("host2:5432/db").AnyTimes()

	// Настройка провайдеров (вызываются при инициализации или каскадно)
	mockPhys1.EXPECT().WithTracerProvider(gomock.Any()).Return(mockPhys1).AnyTimes()
	mockPhys2.EXPECT().WithTracerProvider(gomock.Any()).Return(mockPhys2).AnyTimes()

	t.Run("Cluster with CQRSConnectors", func(t *testing.T) {
		// Оборачиваем физику в CQRS
		cqrs1 := NewCQRSConnector(mockPhys1)
		cqrs2 := NewCQRSConnector(mockPhys2)

		// ИСПРАВЛЕНО: NewPgCluster принимает только ctx и слайс инстансов
		cluster, err := NewPgCluster(ctx, []PgInstance{cqrs1, cqrs2})
		require.NoError(t, err)
		assert.Equal(t, byte(2), cluster.Size())

		// Проверяем String() и иерархию имен
		shard0, _ := cluster.GetShard(0)
		expectedString := "<Shard:0>{<CQRSConnector>{Master:host1:5432/db, Replicas:0}}"
		assert.Equal(t, expectedString, shard0.String())

		// Тестируем выполнение через шард
		query := NewQuery("SELECT 1", func(u *int) []any { return []any{u} }).AsRead()

		// ИСПРАВЛЕНО: Fetch возвращает iter.Seq2[any, error]
		mockPhys2.EXPECT().
			Fetch(gomock.Any(), query, gomock.Any()).
			Return(func(yield func(any, error) bool) {
				val := 42
				yield(&val, nil)
			})

		inst, err := cluster.GetShard(1)
		require.NoError(t, err)

		var results []int
		// Используем Fetch из pgc.go для типизации
		for val, err := range Fetch(ctx, inst, query, 1) {
			require.NoError(t, err)
			results = append(results, val)
		}

		assert.ElementsMatch(t, []int{42}, results)
	})

	t.Run("Cluster Deduplication and Lifecycle", func(t *testing.T) {
		mockPhys1.EXPECT().IsOnline().Return(true).AnyTimes()

		cluster, err := NewPgCluster(ctx, []PgInstance{mockPhys1, mockPhys1})
		require.NoError(t, err)
		assert.Equal(t, byte(2), cluster.Size())

		// Close должен вызваться у физики только 1 раз благодаря uniqueInstances
		mockPhys1.EXPECT().Close(gomock.Any()).Return(nil).Times(1)

		err = cluster.Close(ctx)
		assert.NoError(t, err)
	})

	t.Run("ShardID Routing", func(t *testing.T) {
		cluster, _ := NewPgCluster(ctx, []PgInstance{mockPhys1, mockPhys2})

		s1 := cluster.ShardID("user_123")
		s2 := cluster.ShardID(42)

		assert.True(t, s1 < 2)
		assert.True(t, s2 < 2)
	})
}

func TestPgCluster_RunMigrations_Uniqueness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	mockPhys := NewMockPgInstance(ctrl)

	dsn := "host:5432/db"
	mockPhys.EXPECT().String().Return(dsn).AnyTimes()
	mockPhys.EXPECT().WithTracerProvider(gomock.Any()).Return(mockPhys).AnyTimes()

	// 3 логических шарда на одном физическом инстансе
	shardList := []PgInstance{mockPhys, mockPhys, mockPhys}
	cluster, err := NewPgCluster(ctx, shardList)
	require.NoError(t, err)

	// RunMigrations должен быть вызван только 1 раз на физике
	mockPhys.EXPECT().
		RunMigrations(gomock.Any()).
		Return(nil).
		Times(1)

	err = cluster.RunMigrations(ctx)
	assert.NoError(t, err)
}
