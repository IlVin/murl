package repository

import (
	"context"
	"errors"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

// --- Тесты NewRepoDrv ---

func TestNewRepoDrv(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx := context.Background()
	cfg := NewMockRepoDrvConfig(ctrl)

	t.Run("InMemory Success", func(t *testing.T) {
		cfg.EXPECT().RepoDrv().Return("InMemory")
		cfg.EXPECT().ShardSize().Return(byte(1))
		drv, err := NewRepoDrv(ctx, cfg)
		assert.NoError(t, err)
		assert.NotNil(t, drv)
	})

	t.Run("PgDB Init Error (Bad DSN)", func(t *testing.T) {
		cfg.EXPECT().RepoDrv().Return("PgDB")
		cfg.EXPECT().ShardSize().Return(byte(1))
		cfg.EXPECT().DBDSN().Return("invalid-dsn")
		drv, err := NewRepoDrv(ctx, cfg)
		assert.Error(t, err)
		assert.Nil(t, drv)
	})

}

// --- Тесты InMemoryRepoDrv ---

func TestInMemoryRepoDrv(t *testing.T) {
	ctrl := gomock.NewController(t)
	cfg := NewMockRepoDrvConfig(ctrl)
	cfg.EXPECT().ShardSize().Return(byte(2))
	drv := newInMemoryRepoDrv(cfg)
	ctx := context.Background()

	t.Run("UpSert and Bounds", func(t *testing.T) {
		// Успешная вставка
		id, err := drv.UpSert(ctx, 0, "url1")
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), id)

		// Повторная вставка (RLock branch)
		id2, err := drv.UpSert(ctx, 0, "url1")
		assert.NoError(t, err)
		assert.Equal(t, id, id2)

		// Ошибка границ
		_, err = drv.UpSert(ctx, 5, "url")
		assert.Error(t, err)
	})

	t.Run("Set and lastIdx Logic", func(t *testing.T) {
		// Установка существующего (перезапись индекса)
		_ = drv.Set(ctx, 0, 0, "url1-new")

		// Установка нового с прыжком lastIdx
		err := drv.Set(ctx, 1, 100, "url100")
		assert.NoError(t, err)

		// Проверка Select
		res, err := drv.Select(ctx, 1, 100)
		assert.NoError(t, err)
		assert.Equal(t, "url100", res)

		// Проверка, что UpSert теперь выдаст 101
		id, _ := drv.UpSert(ctx, 1, "url101")
		assert.Equal(t, uint64(101), id)
	})

	t.Run("Select Errors", func(t *testing.T) {
		_, err := drv.Select(ctx, 0, 999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "record not found")

		_, err = drv.Select(ctx, 10, 1)
		assert.Error(t, err)
	})

	t.Run("Set Bounds Error", func(t *testing.T) {
		err := drv.Set(ctx, 10, 1, "url")
		assert.Error(t, err)
	})
}

// --- Тесты PgRepoDrv ---

func TestPgRepoDrv(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHndl := NewMockDBHandler(ctrl)
	drv := &PgRepoDrv{shards: []DBHandler{mockHndl}}
	ctx := context.Background()

	t.Run("UpSert Success", func(t *testing.T) {
		mTx := NewMockTx(ctrl)
		mRow := NewMockRow(ctrl)

		mockHndl.EXPECT().Tx(ctx, gomock.Any()).DoAndReturn(
			func(ctx context.Context, cb func(context.Context, pgx.Tx) error) error {
				return cb(ctx, mTx)
			})
		mTx.EXPECT().QueryRow(gomock.Any(), gomock.Any(), "test-url").Return(mRow)
		mRow.EXPECT().Scan(gomock.Any()).DoAndReturn(func(dest ...interface{}) error {
			*(dest[0].(*uint64)) = 777
			return nil
		})

		id, err := drv.UpSert(ctx, 0, "test-url")
		assert.NoError(t, err)
		assert.Equal(t, uint64(777), id)
	})

	t.Run("UpSert DB Error", func(t *testing.T) {
		mockHndl.EXPECT().Tx(ctx, gomock.Any()).Return(errors.New("db fail"))
		_, err := drv.UpSert(ctx, 0, "url")
		assert.Error(t, err)
	})

	t.Run("Pg Bounds Errors", func(t *testing.T) {
		_, err := drv.UpSert(ctx, 10, "url")
		assert.Error(t, err)
		err = drv.Set(ctx, 10, 1, "url")
		assert.Error(t, err)
		_, err = drv.Select(ctx, 10, 1)
		assert.Error(t, err)
	})
}
