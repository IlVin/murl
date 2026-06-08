package repository

import (
	"context"
	"errors"
	"testing"

	"murl/internal/dto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// Тестируем глобальные переменные пакета
func TestRepo_Globals(t *testing.T) {
	t.Run("Error string", func(t *testing.T) {
		assert.Equal(t, "internal server error", ErrInternalServerError.Error())
	})

	t.Run("ErrInternalServerError is a sentinel error", func(t *testing.T) {
		err := errors.New("some error")
		assert.NotEqual(t, ErrInternalServerError, err)
		assert.True(t, errors.Is(ErrInternalServerError, ErrInternalServerError))
	})
}

// TestRepoInterface тестирует, что все методы интерфейса Repo могут быть вызваны
func TestRepoInterface(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepo(ctrl)

	ctx := context.Background()

	// Создаем тестовые UUID
	sessionID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

	t.Run("Ping method", func(t *testing.T) {
		mock.EXPECT().Ping(ctx).Return(nil).Times(1)
		err := mock.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("Ping method with error", func(t *testing.T) {
		mock.EXPECT().Ping(ctx).Return(assert.AnError).Times(1)
		err := mock.Ping(ctx)
		assert.Error(t, err)
	})

	t.Run("Close method", func(t *testing.T) {
		mock.EXPECT().Close(ctx).Return(nil).Times(1)
		err := mock.Close(ctx)
		assert.NoError(t, err)
	})

	t.Run("Close method with error", func(t *testing.T) {
		mock.EXPECT().Close(ctx).Return(assert.AnError).Times(1)
		err := mock.Close(ctx)
		assert.Error(t, err)
	})

	t.Run("AddURL method", func(t *testing.T) {
		req := dto.AddURL{OriginalURL: "http://example.com"}
		expected := dto.AddURL{ShortURL: "http://short.url/abc123", OriginalURL: req.OriginalURL}

		mock.EXPECT().AddURL(ctx, req).Return(expected, nil).Times(1)
		result, err := mock.AddURL(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("AddURL method with error", func(t *testing.T) {
		req := dto.AddURL{OriginalURL: "http://example.com"}

		mock.EXPECT().AddURL(ctx, req).Return(dto.AddURL{}, assert.AnError).Times(1)
		_, err := mock.AddURL(ctx, req)

		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
	})

	t.Run("AddURLBySessionID method", func(t *testing.T) {
		req := dto.AddURLBySessionID{
			AddURL: dto.AddURL{
				OriginalURL: "http://example.com",
			},
			SessionID: sessionID,
		}
		expected := dto.AddURLBySessionID{
			AddURL: dto.AddURL{
				ShortURL:    "http://short.url/xyz789",
				OriginalURL: req.OriginalURL,
			},
			SessionID: sessionID,
		}

		mock.EXPECT().AddURLBySessionID(ctx, req).Return(expected, nil).Times(1)
		result, err := mock.AddURLBySessionID(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("AddURLBySessionID method with error", func(t *testing.T) {
		req := dto.AddURLBySessionID{
			AddURL: dto.AddURL{
				OriginalURL: "http://example.com",
			},
			SessionID: sessionID,
		}

		mock.EXPECT().AddURLBySessionID(ctx, req).Return(dto.AddURLBySessionID{}, assert.AnError).Times(1)
		_, err := mock.AddURLBySessionID(ctx, req)

		assert.Error(t, err)
	})

	t.Run("GetURL method", func(t *testing.T) {
		req := dto.GetURL{ShortURL: "abc123"}
		expected := dto.GetURL{OriginalURL: "http://example.com", ShortURL: req.ShortURL}

		mock.EXPECT().GetURL(ctx, req).Return(expected, nil).Times(1)
		result, err := mock.GetURL(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("GetURL method with error", func(t *testing.T) {
		req := dto.GetURL{ShortURL: "abc123"}

		mock.EXPECT().GetURL(ctx, req).Return(dto.GetURL{}, assert.AnError).Times(1)
		_, err := mock.GetURL(ctx, req)

		assert.Error(t, err)
	})

	t.Run("GetInternalStats method", func(t *testing.T) {
		expected := dto.Stats{URLs: 100, Users: 10}

		mock.EXPECT().GetInternalStats(ctx).Return(expected, nil).Times(1)
		result, err := mock.GetInternalStats(ctx)

		assert.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("GetInternalStats method with error", func(t *testing.T) {
		mock.EXPECT().GetInternalStats(ctx).Return(dto.Stats{}, assert.AnError).Times(1)
		_, err := mock.GetInternalStats(ctx)

		assert.Error(t, err)
	})

	t.Run("GetURLBySessionID method", func(t *testing.T) {
		req := dto.GetURLBySessionID{SessionID: sessionID}
		expected := dto.GetURLBySessionID{
			SessionID: sessionID,
			Result: []dto.URLItem{
				{ShortURL: "abc123", OriginalURL: "http://example.com/1"},
				{ShortURL: "def456", OriginalURL: "http://example.com/2"},
			},
		}

		mock.EXPECT().GetURLBySessionID(ctx, req).Return(expected, nil).Times(1)
		result, err := mock.GetURLBySessionID(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, expected, result)
		assert.Len(t, result.Result, 2)
	})

	t.Run("GetURLBySessionID method with error", func(t *testing.T) {
		req := dto.GetURLBySessionID{SessionID: sessionID}

		mock.EXPECT().GetURLBySessionID(ctx, req).Return(dto.GetURLBySessionID{}, assert.AnError).Times(1)
		_, err := mock.GetURLBySessionID(ctx, req)

		assert.Error(t, err)
	})

	t.Run("DeleteURLBySessionID method", func(t *testing.T) {
		req := dto.DeleteURLBySessionID{
			SessionID: sessionID,
			ShortURLs: []string{"abc123", "def456"},
		}

		mock.EXPECT().DeleteURLBySessionID(ctx, req).Return(nil).Times(1)
		err := mock.DeleteURLBySessionID(ctx, req)

		assert.NoError(t, err)
	})

	t.Run("DeleteURLBySessionID method with error", func(t *testing.T) {
		req := dto.DeleteURLBySessionID{SessionID: sessionID, ShortURLs: []string{"abc123"}}

		mock.EXPECT().DeleteURLBySessionID(ctx, req).Return(assert.AnError).Times(1)
		err := mock.DeleteURLBySessionID(ctx, req)

		assert.Error(t, err)
	})

	t.Run("Batch method", func(t *testing.T) {
		req := dto.Batch{
			Batch: []dto.BatchItem{
				{CorrelationID: "item1", OriginalURL: "http://example.com/1"},
				{CorrelationID: "item2", OriginalURL: "http://example.com/2"},
			},
		}
		expected := dto.Batch{
			Batch: []dto.BatchItem{
				{CorrelationID: "item1", ShortURL: "http://short.url/abc"},
				{CorrelationID: "item2", ShortURL: "http://short.url/def"},
			},
		}

		mock.EXPECT().Batch(ctx, req).Return(expected, nil).Times(1)
		result, err := mock.Batch(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("Batch method with error", func(t *testing.T) {
		req := dto.Batch{
			Batch: []dto.BatchItem{
				{CorrelationID: "item1", OriginalURL: "http://example.com/1"},
			},
		}

		mock.EXPECT().Batch(ctx, req).Return(dto.Batch{}, assert.AnError).Times(1)
		_, err := mock.Batch(ctx, req)

		assert.Error(t, err)
	})
}

// Создаем простую структуру, реализующую интерфейс
type testConfig struct {
	repoDrv          string
	shardSize        byte
	dbDSN            string
	eventStoragePath string
}

// Реализуем методы для testConfig
func (c testConfig) RepoDrv() string          { return c.repoDrv }
func (c testConfig) ShardSize() byte          { return c.shardSize }
func (c testConfig) DBDSN() string            { return c.dbDSN }
func (c testConfig) EventStoragePath() string { return c.eventStoragePath }

// TestRepoConfigInterface тестирует интерфейс RepoConfig
func TestRepoConfigInterface(t *testing.T) {

	t.Run("RepoConfig interface compliance", func(t *testing.T) {
		cfg := testConfig{
			repoDrv:          "PgDB",
			shardSize:        64,
			dbDSN:            "postgres://localhost:5432/db",
			eventStoragePath: "/tmp/events.log",
		}

		// Проверяем что структура реализует интерфейс
		var _ RepoConfig = cfg

		assert.Equal(t, "PgDB", cfg.RepoDrv())
		assert.Equal(t, byte(64), cfg.ShardSize())
		assert.Equal(t, "postgres://localhost:5432/db", cfg.DBDSN())
		assert.Equal(t, "/tmp/events.log", cfg.EventStoragePath())
	})

	t.Run("RepoConfig with empty values", func(t *testing.T) {
		cfg := testConfig{
			repoDrv:          "",
			shardSize:        0,
			dbDSN:            "",
			eventStoragePath: "",
		}

		var _ RepoConfig = cfg

		assert.Empty(t, cfg.RepoDrv())
		assert.Equal(t, byte(0), cfg.ShardSize())
		assert.Empty(t, cfg.DBDSN())
		assert.Empty(t, cfg.EventStoragePath())
	})
}

// TestRepoInterfaceWithDifferentContexts тестирует вызовы с разными контекстами
func TestRepoInterfaceWithDifferentContexts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepo(ctrl)

	ctx1 := context.Background()
	ctx2, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ожидаем вызовы с разными контекстами
	mock.EXPECT().Ping(ctx1).Return(nil).Times(1)
	mock.EXPECT().Ping(ctx2).Return(assert.AnError).Times(1)

	err := mock.Ping(ctx1)
	assert.NoError(t, err)

	err = mock.Ping(ctx2)
	assert.Error(t, err)
}

// TestRepoInterface_CancelledContext тестирует поведение с отмененным контекстом
func TestRepoInterface_CancelledContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepo(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Отменяем контекст сразу

	// Ожидаем, что метод вернет ошибку контекста
	expectedErr := context.Canceled
	mock.EXPECT().Ping(ctx).Return(expectedErr).Times(1)

	err := mock.Ping(ctx)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

// TestRepoInterface_AnyContext тестирует использование gomock.Any() для контекста
func TestRepoInterface_AnyContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepo(ctrl)

	// Используем gomock.Any() для любого контекста
	mock.EXPECT().Ping(gomock.Any()).Return(nil).Times(3)

	ctx1 := context.Background()
	ctx2, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx3, cancel2 := context.WithTimeout(context.Background(), 100)
	defer cancel2()

	err := mock.Ping(ctx1)
	assert.NoError(t, err)

	err = mock.Ping(ctx2)
	assert.NoError(t, err)

	err = mock.Ping(ctx3)
	assert.NoError(t, err)
}

// TestRepoInterface_Any тестирует использование gomock.Any() для любых параметров
func TestRepoInterface_Any(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepo(ctrl)

	// Используем gomock.Any() для всех параметров
	mock.EXPECT().AddURL(gomock.Any(), gomock.Any()).Return(dto.AddURL{ShortURL: "any"}, nil).Times(1)

	result, err := mock.AddURL(context.Background(), dto.AddURL{OriginalURL: "anything"})

	assert.NoError(t, err)
	assert.Equal(t, "any", result.ShortURL)
}
