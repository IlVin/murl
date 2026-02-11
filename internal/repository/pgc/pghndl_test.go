package pgc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
)

func TestPgHndl_Ping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProv := NewMockpgPoolProvider(ctrl)
	h := &PgHndl{
		name:       "test-db",
		host:       "localhost:5432",
		pgPoolProv: mockProv,
	}
	// Инициализируем атомарные значения
	h.isReady.Store(false)
	h.isClosed.Store(false)
	h.lastCheckResult.Store(checkResult{timestamp: time.Now().Add(-10 * time.Second)})

	t.Run("ping success", func(t *testing.T) {
		ctx := context.Background()

		// Ожидаем вызов Ping. Внутри Ping используется context.WithoutCancel,
		// поэтому проверяем через gomock.Any()
		mockProv.EXPECT().Ping(gomock.Any()).Return(nil)

		err := h.Ping(ctx)
		assert.NoError(t, err)
		assert.True(t, h.IsReady())
	})

	t.Run("ping failure", func(t *testing.T) {
		h.Online() // Принудительно ставим в Online перед тестом
		h.lastCheckResult.Store(checkResult{timestamp: time.Now().Add(-10 * time.Second)})

		// Имитируем сетевую ошибку
		netErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("timeout")}
		mockProv.EXPECT().Ping(gomock.Any()).Return(netErr)

		err := h.Ping(context.Background())
		assert.Error(t, err)
		assert.False(t, h.IsReady(), "Should go offline on network error")
	})
}

func TestPgHndl_Tx(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProv := NewMockpgPoolProvider(ctrl)
	mockTx := NewMockTx(ctrl) // Сгенерированный мок для pgx.Tx

	h := &PgHndl{
		pgPoolProv: mockProv,
	}
	h.isReady.Store(true)

	t.Run("transaction success", func(t *testing.T) {
		ctx := context.Background()

		// 1. Ожидаем начало транзакции
		mockProv.EXPECT().Begin(ctx).Return(mockTx, nil)

		// 2. Ожидаем коммит
		mockTx.EXPECT().Commit(gomock.Any()).Return(nil)

		res, err := h.Tx(ctx, func(ctx context.Context, tx pgx.Tx) (any, error) {
			assert.Equal(t, mockTx, tx)
			return "ok", nil
		})

		assert.NoError(t, err)
		assert.Equal(t, "ok", res)
	})

	t.Run("transaction rollback on error", func(t *testing.T) {
		ctx := context.Background()

		mockProv.EXPECT().Begin(ctx).Return(mockTx, nil)

		// Ожидаем Rollback, так как колбэк вернет ошибку
		mockTx.EXPECT().Rollback(gomock.Any()).Return(nil)

		_, err := h.Tx(ctx, func(ctx context.Context, tx pgx.Tx) (any, error) {
			return nil, errors.New("query failed")
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query failed")
	})
}

func TestPgHndl_RecoverPanic(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProv := NewMockpgPoolProvider(ctrl)
	h := &PgHndl{pgPoolProv: mockProv}
	h.isReady.Store(true)

	t.Run("recover in PgPool", func(t *testing.T) {
		err := h.PgPool(context.Background(), func(ctx context.Context, pool *pgxpool.Pool) error {
			panic("something went wrong")
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "panic recovered")
	})
}

func TestPgHndl_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProv := NewMockpgPoolProvider(ctrl)
	h := &PgHndl{pgPoolProv: mockProv}
	h.isReady.Store(true)

	mockProv.EXPECT().Close().Times(1)

	h.Close()
	assert.False(t, h.IsReady())
	assert.True(t, h.isClosed.Load())
}
