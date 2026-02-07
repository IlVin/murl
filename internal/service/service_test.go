package service

import (
	"context"
	"fmt"
	"murl/internal/config"
	"murl/internal/model"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

type mockServiceConfig struct {
	baseURL config.ShortBaseURL
}

func (m *mockServiceConfig) ShortBaseURL() config.ShortBaseURL { return m.baseURL }

type mockRepo struct {
	mock.Mock
}

func (m *mockRepo) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(1)
}

func (m *mockRepo) Save(lURL string) (byte, uint64, error) {
	args := m.Called(lURL)
	return args.Get(0).(byte), args.Get(1).(uint64), args.Error(2)
}

func (m *mockRepo) Load(sID byte, idx uint64) (string, error) {
	args := m.Called(sID, idx)
	return args.String(0), args.Error(1)
}

// --- Tests ---

func TestService_AddURL(t *testing.T) {
	baseURL, _ := url.Parse("https://m.url")
	cfg := &mockServiceConfig{baseURL: config.ShortBaseURL{URL: *baseURL}}

	t.Run("invalid url format", func(t *testing.T) {
		s := NewService(cfg, new(mockRepo))
		// Символ \x7f (DEL) или управляющие символы в начале заставляют url.Parse выдать ошибку
		_, err := s.AddURL("http://example.com/\x7f")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid URL format")
	})

	t.Run("not absolute URL", func(t *testing.T) {
		s := NewService(cfg, new(mockRepo))
		_, err := s.AddURL("/relative/path")
		assert.Error(t, err)
		assert.Equal(t, "URL is not absolute", err.Error())
	})

	t.Run("unsupported scheme", func(t *testing.T) {
		s := NewService(cfg, new(mockRepo))
		_, err := s.AddURL("ftp://files.com")
		assert.Error(t, err)
		assert.Equal(t, "unsupported protocol scheme", err.Error())
	})

	t.Run("repo save error", func(t *testing.T) {
		mRepo := new(mockRepo)
		s := NewService(cfg, mRepo)
		mRepo.On("Save", "http://google.com").Return(byte(0), uint64(0), fmt.Errorf("db error")).Once()

		_, err := s.AddURL("http://google.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to persist data")
	})

	t.Run("model make error", func(t *testing.T) {
		mRepo := new(mockRepo)
		s := NewService(cfg, mRepo)
		// ShardID 64 недопустим для словаря (макс 63), это вызовет ошибку в model.MakeShortURL
		mRepo.On("Save", "http://google.com").Return(byte(64), uint64(1), nil).Once()

		_, err := s.AddURL("http://google.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to persist data")
	})

	t.Run("success", func(t *testing.T) {
		mRepo := new(mockRepo)
		s := NewService(cfg, mRepo)
		mRepo.On("Save", "https://yandex.ru").Return(byte(1), uint64(10), nil).Once()

		sURL, err := s.AddURL("https://yandex.ru")
		assert.NoError(t, err)
		assert.NotEmpty(t, sURL)
	})
}

func TestService_GetURL(t *testing.T) {
	baseURL, _ := url.Parse("https://m.url")
	cfg := &mockServiceConfig{baseURL: config.ShortBaseURL{URL: *baseURL}}

	t.Run("invalid short URL format", func(t *testing.T) {
		s := NewService(cfg, new(mockRepo))
		// Передаем строку, которую model.ParseShortURL не сможет распарсить
		_, err := s.GetURL("http://short.url/\x7f")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid URL format")
	})

	t.Run("repo load error", func(t *testing.T) {
		mRepo := new(mockRepo)
		s := NewService(cfg, mRepo)

		// Генерируем ПРАВИЛЬНУЮ ссылку через модель (Shard 0, Index 10)
		u, _ := url.Parse("https://m.url")
		sURL, _ := model.MakeShortURL(0, 10, u)

		mRepo.On("Load", byte(0), uint64(10)).Return("", fmt.Errorf("not found")).Once()

		_, err := s.GetURL(sURL)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to retrieve data")
	})

	t.Run("success", func(t *testing.T) {
		mRepo := new(mockRepo)
		s := NewService(cfg, mRepo)

		longURL := "https://apple.com"
		// Шард 1 ('B'), Индекс 1 ('C')
		u, _ := url.Parse("https://m.url")
		sURL, _ := model.MakeShortURL(1, 1, u)

		mRepo.On("Load", byte(1), uint64(1)).Return(longURL, nil).Once()

		res, err := s.GetURL(sURL)
		assert.NoError(t, err)
		assert.Equal(t, longURL, res)
	})
}
