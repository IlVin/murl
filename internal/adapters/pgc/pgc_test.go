package pgc

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"testing"

	pgconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type Res struct {
	ID int
}

func resBinder(r *Res) []any {
	return []any{&r.ID}
}

func TestNewQuery_Audits(t *testing.T) {
	t.Run("panics on empty sql", func(t *testing.T) {
		assert.Panics(t, func() {
			NewQuery("", resBinder)
		})
	})
}

func TestQuery_IntegrationStyle(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPg := NewMockPgInstance(ctrl)

	t.Run("FetchRow usage", func(t *testing.T) {
		q := NewQuery("SELECT id FROM users WHERE id = $1", resBinder)
		expected := &Res{ID: 42}

		// Теперь мокаем FetchRow самого PgInstance
		mockPg.EXPECT().
			FetchRow(ctx, q, 42).
			Return(expected, nil)

		// Используем новый типизированный API
		res, err := FetchRow(ctx, mockPg, q, 42)

		require.NoError(t, err)
		assert.Equal(t, 42, res.ID)
	})
}

func TestQuery_Metadata(t *testing.T) {
	q := NewQuery("SELECT id FROM users", resBinder)

	assert.Equal(t, "SELECT id FROM users", q.SQL())
	assert.True(t, q.HasReturns())

	// Проверка NewTarget (стирание типа)
	target := q.NewTarget()
	assert.IsType(t, &Res{}, target)

	// Проверка Binder
	fields := q.Binder(target)
	assert.Len(t, fields, 1)
}

func TestQuery_StatusManual(t *testing.T) {
	t.Run("default status is write", func(t *testing.T) {
		q := NewQuery[Res]("SELECT id FROM users", resBinder)
		assert.False(t, q.IsReadOnly(), "запрос по умолчанию должен быть Write")
	})

	t.Run("manual switch to read", func(t *testing.T) {
		// Используем чейн методов
		q := NewQuery[Res]("SELECT id FROM users", resBinder).AsRead()
		assert.True(t, q.IsReadOnly())
	})

	t.Run("manual switch to write", func(t *testing.T) {
		q := NewQuery[Res]("INSERT...", nil).AsWrite()
		assert.False(t, q.IsReadOnly())
	})
}

func TestFetchRow_WithManualQuery(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPg := NewMockPgInstance(ctrl)
	q := NewQuery[Res]("SELECT id...", resBinder).AsRead()

	expected := &Res{ID: 100}
	mockPg.EXPECT().
		FetchRow(gomock.Any(), q, gomock.Any()).
		Return(expected, nil)

	res, err := FetchRow(context.Background(), mockPg, q, 1)
	assert.NoError(t, err)
	assert.Equal(t, 100, res.ID)
}

type mockNetError struct{ error }

func (e mockNetError) Timeout() bool   { return true }
func (e mockNetError) Temporary() bool { return true }

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Nil error is not retryable",
			err:      nil,
			expected: false,
		},
		{
			name:     "Generic error is not retryable",
			err:      errors.New("some random error"),
			expected: false,
		},
		{
			name:     "Standard net.Error is retryable",
			err:      mockNetError{errors.New("network failure")},
			expected: true,
		},
		{
			name:     "Wrapped net.Error is retryable",
			err:      fmt.Errorf("wrapped: %w", mockNetError{errors.New("timeout")}),
			expected: true,
		},
		{
			name:     "Postgres Admin Shutdown (57P01) is retryable",
			err:      &pgconn.PgError{Code: "57P01"},
			expected: true,
		},
		{
			name:     "Postgres Too Many Connections (53300) is retryable",
			err:      &pgconn.PgError{Code: "53300"},
			expected: true,
		},
		{
			name:     "Postgres Syntax Error (42601) is NOT retryable",
			err:      &pgconn.PgError{Code: "42601"},
			expected: false,
		},
		{
			name:     "Wrapped PgError is retryable",
			err:      fmt.Errorf("db fail: %w", &pgconn.PgError{Code: "57P03"}),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isRetryable(tt.err))
		})
	}
}

func TestGetInstrumentationName(t *testing.T) {
	// 1. Получаем ожидаемое имя модуля из текущего окружения
	expectedPrefix := "pgc"
	if buildInfo, ok := debug.ReadBuildInfo(); ok && buildInfo.Main.Path != "" {
		parts := strings.Split(buildInfo.Main.Path, "/")
		expectedPrefix = parts[len(parts)-1] + ".pgc"
	}

	t.Run("without subsystem", func(t *testing.T) {
		name := getInstrumentationName("")
		assert.Equal(t, expectedPrefix, name)
	})

	t.Run("with subsystem", func(t *testing.T) {
		sub := "connector"
		name := getInstrumentationName(sub)
		assert.Equal(t, expectedPrefix+"."+sub, name)
	})

	t.Run("consistency check", func(t *testing.T) {
		// Проверяем, что нет лишних точек или пустых сегментов
		name := getInstrumentationName("shard")
		parts := strings.Split(name, ".")

		for _, part := range parts {
			assert.NotEmpty(t, part, "Имя не должно содержать пустых сегментов (двойных точек)")
		}
		assert.True(t, len(parts) >= 2, "Имя должно состоять минимум из [module].pgc")
	})
}
