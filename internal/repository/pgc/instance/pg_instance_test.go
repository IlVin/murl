package instance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"murl/internal/mocks"
	"murl/internal/repository/pgc"
	"murl/internal/repository/pgc/backoff"
	"murl/internal/repository/pgc/fcounter"
)

func TestPgInstance_Tx_PoolBeginError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := mocks.NewMockpgxPoolDriverIface(ctrl)

	h := &pgInstance{
		pgPool:   mockPool,
		failures: fcounter.NewFailureCounter(3, time.Second),
		repeater: backoff.NewPgBackoff(1, time.Second),
	}
	h.isReady.Store(true)

	ctx := context.Background()
	testErr := errors.New("connection refused")

	// Проверяем только этап Begin.
	// Так как tx будет nil, Rollback в defer не упадет.
	mockPool.EXPECT().Begin(ctx).Return(nil, testErr)

	err := h.Tx(ctx, func(ctx context.Context, tx pgc.PgxTxIface) error {
		return nil
	})

	assert.ErrorIs(t, err, testErr)
}

func TestPgInstance_PgPool_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := mocks.NewMockpgxPoolDriverIface(ctrl)

	h := &pgInstance{
		pgPool:   mockPool,
		failures: fcounter.NewFailureCounter(3, time.Second),
		repeater: backoff.NewPgBackoff(1, time.Second),
	}
	h.isReady.Store(true)

	ctx := context.Background()

	// PgPool просто пробрасывает пул в коллбэк
	err := h.PgPool(ctx, func(ctx context.Context, pool pgc.PgxPoolIface) error {
		assert.Equal(t, mockPool, pool)
		return nil
	})

	assert.NoError(t, err)
}

func TestPgInstance_CircuitBreaker_Activation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := mocks.NewMockpgxPoolDriverIface(ctrl)

	// 1 ошибка и мы Offline
	h := &pgInstance{
		pgPool:   mockPool,
		failures: fcounter.NewFailureCounter(1, time.Second),
		repeater: backoff.NewPgBackoff(1, time.Second),
	}
	h.isReady.Store(true)

	ctx := context.Background()
	mockPool.EXPECT().Begin(ctx).Return(nil, errors.New("fatal"))

	// Первый вызов активирует CB
	_ = h.Tx(ctx, func(ctx context.Context, tx pgc.PgxTxIface) error { return nil })

	assert.False(t, h.IsReady())

	// Второй вызов даже не дойдет до пула
	err := h.Tx(ctx, func(ctx context.Context, tx pgc.PgxTxIface) error { return nil })
	assert.Contains(t, err.Error(), "PgInstance is not ready")
}
