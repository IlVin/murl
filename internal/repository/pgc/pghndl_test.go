package pgc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestPgHndl(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := NewMockPgxPoolIface(ctrl)
	ctx := context.Background()

	// Вспомогательная функция для инициализации хендла с моком
	newTestHndl := func() *PgHndl {
		h := &PgHndl{
			name:   "test-db",
			pgPool: mockPool,
		}
		h.lastCheckResult.Store(checkResult{err: nil})
		h.isReady.Store(true)
		return h
	}

	t.Run("Ping_Caching", func(t *testing.T) {
		h := newTestHndl()
		h.lastCheckTime.Store(time.Now().UnixNano())

		// Должен вернуть nil без вызова mockPool.Ping, так как прошло < 3 сек
		err := h.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("Ping_LockContention", func(t *testing.T) {
		h := newTestHndl()
		h.checkInProgress.Lock() // Симулируем занятый лок другим процессом
		defer h.checkInProgress.Unlock()

		err := h.Ping(ctx)
		assert.NoError(t, err) // Возвращает последний результат из атомика
	})

	t.Run("Ping_Timeout_Offline", func(t *testing.T) {
		h := newTestHndl()
		h.lastCheckTime.Store(0)

		mockPool.EXPECT().Ping(gomock.Any()).Return(context.DeadlineExceeded)

		err := h.Ping(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.False(t, h.IsReady())
	})

	t.Run("Begin_Offline_Ping_Fail", func(t *testing.T) {
		h := newTestHndl()
		h.Offline()
		h.lastCheckTime.Store(0)

		mockPool.EXPECT().Ping(gomock.Any()).Return(errors.New("still down"))

		err := h.Begin(ctx, func(tx pgx.Tx) error { return nil })
		assert.Contains(t, err.Error(), "database connection is offline")
	})

}

func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("random"), false},
		{&net.OpError{}, true},
		{&pgconn.ConnectError{}, true},
		{&pgconn.PgError{Code: "08001"}, true},  // Connection Exception
		{&pgconn.PgError{Code: "23505"}, false}, // Unique Violation
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, IsNetworkError(tt.err))
	}
}
