package service

import (
	"errors"
	"murl/internal/config"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockRepo реализует интерфейс MicroURLRepo
type mockRepo struct {
	mock.Mock
}

func (m *mockRepo) Save(lURL string) (byte, uint64, error) {
	args := m.Called(lURL)
	return args.Get(0).(byte), args.Get(1).(uint64), args.Error(2)
}

func (m *mockRepo) Load(sID byte, idx uint64) (string, error) {
	args := m.Called(sID, idx)
	return args.String(0), args.Error(1)
}

// mockCfg реализует ServiceConfig
type mockCfg struct {
	baseURL string
	logger  *zap.Logger
}

func (m mockCfg) ShortBaseURL() config.ShortBaseURL {
	u, _ := url.Parse(m.baseURL)
	return config.ShortBaseURL{URL: *u}
}

func (m mockCfg) Zap() *zap.Logger {
	if m.logger == nil {
		return zap.NewNop()
	}
	return m.logger
}

func TestService_AddURL(t *testing.T) {
	logger := zap.NewNop()
	baseCfg := mockCfg{baseURL: "http://localhost:8080", logger: logger}

	t.Run("success", func(t *testing.T) {
		repo := new(mockRepo)
		svc := NewService(baseCfg, repo)
		longURL := "https://google.com"

		repo.On("Save", longURL).Return(byte(1), uint64(10), nil)

		res, err := svc.AddURL(longURL)
		require.NoError(t, err)
		assert.Contains(t, res, "http://localhost:8080")
		repo.AssertExpectations(t)
	})

	t.Run("parse error", func(t *testing.T) {
		svc := NewService(baseCfg, nil)
		// Управляющий символ в URL вызовет ошибку url.Parse
		_, err := svc.AddURL("http://[::1]:index")
		assert.ErrorIs(t, err, ErrInvalidURL)
	})

	t.Run("not absolute", func(t *testing.T) {
		svc := NewService(baseCfg, nil)
		_, err := svc.AddURL("just/path")
		assert.ErrorIs(t, err, ErrURLNoAbs)
	})

	t.Run("unsupported scheme", func(t *testing.T) {
		svc := NewService(baseCfg, nil)
		_, err := svc.AddURL("ftp://server.com")
		assert.ErrorIs(t, err, ErrUnsupportedScheme)
	})

	t.Run("repo save error", func(t *testing.T) {
		repo := new(mockRepo)
		svc := NewService(baseCfg, repo)
		repo.On("Save", mock.Anything).Return(byte(0), uint64(0), errors.New("db fail"))

		_, err := svc.AddURL("https://valid.com")
		assert.ErrorIs(t, err, ErrStoreSaveFailed)
	})

	t.Run("model error (shard overflow)", func(t *testing.T) {
		repo := new(mockRepo)
		svc := NewService(baseCfg, repo)
		// shardID 100 вызывает ErrShardIDLimit в пакете model
		repo.On("Save", mock.Anything).Return(byte(100), uint64(1), nil)

		_, err := svc.AddURL("https://valid.com")
		assert.ErrorIs(t, err, ErrStoreSaveFailed)
	})
}

func TestService_GetURL(t *testing.T) {
	logger := zap.NewNop()
	baseCfg := mockCfg{baseURL: "http://localhost:8080", logger: logger}

	t.Run("success", func(t *testing.T) {
		repo := new(mockRepo)
		svc := NewService(baseCfg, repo)
		expectedURL := "https://yandex.ru"

		// Моделируем валидную ссылку (Shard A=0, Index 0=A) -> "/AA"
		repo.On("Load", byte(0), uint64(0)).Return(expectedURL, nil)

		res, err := svc.GetURL("http://localhost:8080/AAA")
		require.NoError(t, err)
		assert.Equal(t, expectedURL, res)
	})

	t.Run("model parse error", func(t *testing.T) {
		svc := NewService(baseCfg, nil)
		// Слишком короткий путь вызовет ошибку в model
		_, err := svc.GetURL("http://localhost:8080/A")
		assert.ErrorIs(t, err, ErrInvalidURL)
	})

	t.Run("repo load error", func(t *testing.T) {
		repo := new(mockRepo)
		svc := NewService(baseCfg, repo)
		repo.On("Load", mock.Anything, mock.Anything).Return("", errors.New("not found"))

		_, err := svc.GetURL("http://localhost:8080/AAA")
		assert.ErrorIs(t, err, ErrStoreGetFailed)
	})
}
