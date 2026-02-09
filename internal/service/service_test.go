package service

import (
	"context"
	"errors"
	"murl/internal/config"
	"murl/internal/model"
	"net/url"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCfg реализует ServiceConfig для тестов
type mockCfg struct {
	baseURL string
}

func (m mockCfg) ShortBaseURL() config.ShortBaseURL {
	u, _ := url.Parse(m.baseURL)
	return config.ShortBaseURL{URL: *u}
}

func TestService_AddURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockMicroURLRepo(ctrl)
	base := "https://m.url"
	cfg := mockCfg{baseURL: base}
	svc := NewService(context.Background(), cfg, mockRepo)

	t.Run("Success shortening and validation", func(t *testing.T) {
		longURL := "https://google.com"
		expectedShard := byte(5)
		expectedIdx := uint64(12345)

		// Настраиваем мок
		mockRepo.EXPECT().
			Save(gomock.Any(), longURL).
			Return(expectedShard, expectedIdx, nil)

		// Вызов сервиса
		gotSURL, err := svc.AddURL(context.Background(), longURL)
		require.NoError(t, err)

		// ВАЛИДАЦИЯ: используем сервисную функцию модели для проверки результата
		shardID, idx, err := model.ParseShortURL(gotSURL)
		require.NoError(t, err, "Service generated an invalid URL format")

		assert.Equal(t, expectedShard, shardID)
		assert.Equal(t, expectedIdx, idx)
		assert.Contains(t, gotSURL, base)
	})

	t.Run("Invalid protocol should fail", func(t *testing.T) {
		_, err := svc.AddURL(context.Background(), "ftp://secret.file")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported protocol")
	})
}

func TestService_GetURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockMicroURLRepo(ctrl)
	baseStr := "http://m.url"
	baseURL, _ := url.Parse(baseStr)
	cfg := mockCfg{baseURL: baseStr}
	svc := NewService(context.Background(), cfg, mockRepo)

	t.Run("Success retrieval using model generator", func(t *testing.T) {
		expectedLongURL := "https://github.com"
		shardID := byte(10)
		idx := uint64(987654321)

		// ГЕНЕРАЦИЯ: создаем входной URL с помощью функции модели
		shortURL, err := model.MakeShortURL(shardID, idx, baseURL)
		require.NoError(t, err)

		// Настраиваем мок на те параметры, которые зашиты в сгенерированный URL
		mockRepo.EXPECT().
			Load(gomock.Any(), shardID, idx).
			Return(expectedLongURL, nil)

		// Вызов сервиса
		gotLongURL, err := svc.GetURL(context.Background(), shortURL)

		require.NoError(t, err)
		assert.Equal(t, expectedLongURL, gotLongURL)
	})

	t.Run("Repo error handling", func(t *testing.T) {
		shortURL, _ := model.MakeShortURL(1, 1, baseURL)

		mockRepo.EXPECT().
			Load(gomock.Any(), gomock.Any(), gomock.Any()).
			Return("", errors.New("db connection lost"))

		_, err := svc.GetURL(context.Background(), shortURL)
		assert.Error(t, err)
	})
}

func TestService_Ping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockMicroURLRepo(ctrl)
	svc := NewService(context.Background(), mockCfg{}, mockRepo)

	mockRepo.EXPECT().Ping(gomock.Any()).Return(nil)

	err := svc.Ping(context.Background())
	assert.NoError(t, err)
}
