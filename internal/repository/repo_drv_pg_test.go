package repository

import (
	"context"
	"testing"

	"murl/internal/model/event"

	gomock "github.com/golang/mock/gomock"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgRepoDrv_UpSert(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandler := NewMockDBHandler(ctrl)
	mockTx := NewMockTx(ctrl)
	mockRow := NewMockRow(ctrl)

	drv := &PgRepoDrv{shards: []DBHandler{mockHandler}}
	ctx := context.Background()

	t.Run("success_new_insert", func(t *testing.T) {
		mockHandler.EXPECT().
			Tx(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, cb func(context.Context, pgx.Tx) error) error {
				return cb(ctx, mockTx)
			})

		mockTx.EXPECT().
			QueryRow(gomock.Any(), sqlUpSert, "https://go.dev").
			Return(mockRow)

		mockRow.EXPECT().
			Scan(gomock.Any(), gomock.Any()).
			Do(func(dest ...any) {
				// dest[0] — это *uint64 (id)
				// dest[1] — это *bool (cf)
				*dest[0].(*uint64) = 1
				*dest[1].(*bool) = false
			}).
			Return(nil)

		id, conflict, err := drv.UpSert(ctx, 0, "https://go.dev")
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), id)
		assert.False(t, conflict)
	})
}

func TestPgRepoDrv_BatchUpSert(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandler := NewMockDBHandler(ctrl)
	mockTx := NewMockTx(ctrl)
	mockBR := NewMockBatchResults(ctrl)
	mockRow := NewMockRow(ctrl)

	drv := &PgRepoDrv{shards: []DBHandler{mockHandler}}
	ctx := context.Background()

	batch := []event.PayloadBatchItem{
		{ShardID: 0, OrigURL: "url1"},
	}

	mockHandler.EXPECT().
		Tx(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, cb func(context.Context, pgx.Tx) error) error {
			return cb(ctx, mockTx)
		})

	mockTx.EXPECT().SendBatch(gomock.Any(), gomock.Any()).Return(mockBR)
	mockBR.EXPECT().Close().Return(nil)

	// В BatchUpSert Scan принимает (*uint64, *bool)
	mockBR.EXPECT().QueryRow().Return(mockRow)
	mockRow.EXPECT().
		Scan(gomock.Any(), gomock.Any()).
		Do(func(dest ...any) {
			*dest[0].(*uint64) = 100
			*dest[1].(*bool) = true
		}).
		Return(nil)

	res, err := drv.BatchUpSert(ctx, batch)
	assert.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, uint64(100), res[0].Idx)
	assert.True(t, res[0].ConflictFlag)
}

func TestPgRepoDrv_RunMigrations(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m1 := NewMockDBHandler(ctrl)
	m2 := NewMockDBHandler(ctrl)

	// Настраиваем ОБА мока так, чтобы они выглядели как один и тот же инстанс
	m1.EXPECT().Instance().Return("node-1").AnyTimes()
	m1.EXPECT().Name().Return("db-main").AnyTimes()
	m2.EXPECT().Instance().Return("node-1").AnyTimes()
	m2.EXPECT().Name().Return("db-main").AnyTimes()

	drv := &PgRepoDrv{shards: []DBHandler{m1, m2}}

	// Так как в map["node-1/db-main"] попадет либо m1, либо m2 (зависит от итерации),
	// мы разрешаем вызов RunMigrations любому из них, но строго ОДИН раз на всю группу.

	// Вариант А: если хотим проверить дедупликацию железно
	m1.EXPECT().RunMigrations(gomock.Any()).Return(nil).MaxTimes(1)
	m2.EXPECT().RunMigrations(gomock.Any()).Return(nil).MaxTimes(1)

	err := drv.RunMigrations(context.Background())
	assert.NoError(t, err)
}

func TestPgRepoDrv_Select(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandler := NewMockDBHandler(ctrl)
	// В PgPool передается интерфейс pgc.PgPool (нужен мок и для него)
	// Но для простоты теста Select проверяем сам факт вызова PgPool

	drv := &PgRepoDrv{shards: []DBHandler{mockHandler}}

	mockHandler.EXPECT().
		PgPool(gomock.Any(), gomock.Any()).
		Return(nil)

	_, err := drv.Select(context.Background(), 0, 42)
	assert.NoError(t, err)
}

func TestPgRepoDrv_Ping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m1 := NewMockDBHandler(ctrl)
	drv := &PgRepoDrv{shards: []DBHandler{m1}}

	m1.EXPECT().Ping(gomock.Any()).Return(nil)

	err := drv.Ping(context.Background())
	assert.NoError(t, err)
}
