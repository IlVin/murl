package repository

import (
	"context"
	"errors"
	"testing"

	"murl/internal/model/event"

	gomock "github.com/golang/mock/gomock"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

func TestPgRepoDrv_BatchUpSert_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m0 := NewMockDBHandler(ctrl)
	m1 := NewMockDBHandler(ctrl)

	mockTx := NewMockTx(ctrl)

	// Настраиваем QueryRow так, чтобы он всегда возвращал НОВЫЙ мок Row
	// у которого ЗАРАНЕЕ прописано ожидание Scan
	mockTx.EXPECT().
		QueryRow(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, sql string, args ...any) pgx.Row {
			mRow := NewMockRow(ctrl)
			mRow.EXPECT().Scan(gomock.Any()).Return(nil) // Успешный скан
			return mRow
		}).AnyTimes()

	repo := &PgRepoDrv{shards: []DBHandler{m0, m1}}
	ctx := context.Background()

	batch := []event.PayloadBatchItem{
		{CorrelationID: "c1", OrigURL: "url1", ShardID: 0},
		{CorrelationID: "c2", OrigURL: "url2", ShardID: 1},
		{CorrelationID: "c3", OrigURL: "url3", ShardID: 0},
	}

	// Настройка для Шарда 0
	m0.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, cb func(context.Context, pgx.Tx) (any, error)) (any, error) {
			return cb(ctx, mockTx)
		})

	// Настройка для Шарда 1
	m1.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, cb func(context.Context, pgx.Tx) (any, error)) (any, error) {
			return cb(ctx, mockTx)
		})

	res, err := repo.BatchUpSert(ctx, batch)

	assert.NoError(t, err)
	assert.Len(t, res, 3)
}

func TestPgRepoDrv_BatchUpSert_PartialShardFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m0 := NewMockDBHandler(ctrl)
	repo := &PgRepoDrv{shards: []DBHandler{m0}}
	ctx := context.Background()

	batch := []event.PayloadBatchItem{
		{CorrelationID: "c1", OrigURL: "url1", ShardID: 0},
	}

	// Симулируем ошибку транзакции
	m0.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(nil, errors.New("db disconnect"))

	res, err := repo.BatchUpSert(ctx, batch)

	assert.NoError(t, err) // BatchUpSert не возвращает ошибку, а пишет её в элементы
	assert.NotNil(t, res[0].Err)
	assert.Equal(t, uint64(0), res[0].Idx)
}

func TestPgRepoDrv_UpSert_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m := NewMockDBHandler(ctrl)
	repo := &PgRepoDrv{shards: []DBHandler{m}}

	m.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(nil, errors.New("sql error"))

	id, err := repo.UpSert(context.Background(), 0, "test")
	assert.Error(t, err)
	assert.Equal(t, uint64(0), id)
}

func TestPgRepoDrv_Select_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m := NewMockDBHandler(ctrl)
	repo := &PgRepoDrv{shards: []DBHandler{m}}

	m.EXPECT().PgPool(gomock.Any(), gomock.Any()).Return(nil)

	url, err := repo.Select(context.Background(), 0, 1)
	assert.NoError(t, err)
	assert.Equal(t, "", url)
}
