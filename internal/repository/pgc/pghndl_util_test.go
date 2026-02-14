package pgc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgerrcode"
	pgconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Фиктивная ошибка для тестов
var errTest = errors.New("test error")

func TestNewPgBackoff(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		pb := NewPgBackoff(0, 0)
		assert.Equal(t, 1, pb.attempt)
		assert.Equal(t, minDuration, pb.waitDuration)
		assert.Greater(t, pb.factor, 1.0)
	})

	t.Run("custom", func(t *testing.T) {
		pb := NewPgBackoff(5, time.Second)
		assert.Equal(t, 5, pb.attempt)
		assert.Equal(t, minDuration, pb.waitDuration)
	})
}

func TestWithRetry_Success(t *testing.T) {
	pb := NewPgBackoff(3, time.Second)
	calls := 0
	err := pb.WithRetry(context.Background(), func() error {
		calls++
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestWithRetry_RetriableFailures(t *testing.T) {
	pb := NewPgBackoff(3, 200*time.Millisecond)
	calls := 0

	// Ошибка, которую можно ретраить (сетевая)
	netErr := &net.OpError{Op: "dial", Net: "tcp", Err: errTest}

	err := pb.WithRetry(context.Background(), func() error {
		calls++
		if calls < 3 {
			return netErr
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestWithRetry_NonRetriableFailure(t *testing.T) {
	pb := NewPgBackoff(5, time.Second)
	calls := 0

	// Ошибка нарушения уникальности (нельзя ретраить)
	uniqueErr := &pgconn.PgError{Code: pgerrcode.UniqueViolation}

	err := pb.WithRetry(context.Background(), func() error {
		calls++
		return uniqueErr
	})

	require.Error(t, err)
	assert.Equal(t, 1, calls, "Should stop after first non-retriable error")
	assert.Contains(t, err.Error(), pgerrcode.UniqueViolation)
}

func TestWithRetry_ContextCancel(t *testing.T) {
	pb := NewPgBackoff(5, time.Hour) // Огромная задержка
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := pb.WithRetry(ctx, func() error {
		calls++
		return &pgconn.PgError{Code: pgerrcode.SerializationFailure}
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Equal(t, 1, calls)
}

func TestErrorChecks(t *testing.T) {
	t.Run("IsTimeout", func(t *testing.T) {
		assert.False(t, IsTimeout(nil))
		assert.True(t, IsTimeout(context.DeadlineExceeded))
		assert.True(t, IsTimeout(&net.OpError{Err: &timeoutErr{}}))
		assert.False(t, IsTimeout(errTest))
	})

	t.Run("IsNetworkError", func(t *testing.T) {
		assert.False(t, IsNetworkError(nil))
		assert.True(t, IsNetworkError(&net.OpError{}))
		assert.True(t, IsNetworkError(&pgconn.ConnectError{}))
		assert.True(t, IsNetworkError(&pgconn.PgError{Code: "08001"}))
		assert.False(t, IsNetworkError(&pgconn.PgError{Code: "23505"}))
	})

	t.Run("IsRetriable_PG_Codes", func(t *testing.T) {
		// Класс 40
		assert.True(t, IsRetriable(&pgconn.PgError{Code: pgerrcode.DeadlockDetected}))
		// Класс 57
		assert.True(t, IsRetriable(&pgconn.PgError{Code: pgerrcode.AdminShutdown}))
		// Класс 22 (Data)
		assert.False(t, IsRetriable(&pgconn.PgError{Code: pgerrcode.DataException}))
		// Класс 42 (Syntax)
		assert.False(t, IsRetriable(&pgconn.PgError{Code: pgerrcode.SyntaxError}))
		// Просто ошибка
		assert.False(t, IsRetriable(errTest))
	})
}

func TestWithRetry_MaxAttemptsReached(t *testing.T) {
	pb := NewPgBackoff(2, 100*time.Millisecond)
	calls := 0
	err := pb.WithRetry(context.Background(), func() error {
		calls++
		return &pgconn.PgError{Code: pgerrcode.SerializationFailure}
	})

	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

// Хелпер для симуляции таймаута net.Error
type timeoutErr struct{}

func (e *timeoutErr) Error() string   { return "timeout" }
func (e *timeoutErr) Timeout() bool   { return true }
func (e *timeoutErr) Temporary() bool { return true }
