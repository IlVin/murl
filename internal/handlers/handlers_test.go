package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"murl/internal/model/event"
	"net/http"
	"net/http/httptest"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHandlersConfig для реализации интерфейса HandlersConfig
type mockHandlersConfig struct{}

func TestHandlers_HndlAddURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockMicroURLService(ctrl)
	h := NewHandlers(&mockHandlersConfig{}, mockService)

	t.Run("Success plain text", func(t *testing.T) {
		longURL := "https://yandex.ru"
		shortURL := "http://localhost/1_1"

		mockService.EXPECT().
			AddURL(gomock.Any(), longURL).
			Return(shortURL, nil)

		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(longURL))
		w := httptest.NewRecorder()

		h.HndlAddURL()(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, shortURL, w.Body.String())
	})

	t.Run("Empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		w := httptest.NewRecorder()

		h.HndlAddURL()(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandlers_HndlAPIShortenBatch_Streaming(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockMicroURLService(ctrl)
	h := NewHandlers(&mockHandlersConfig{}, mockService)

	t.Run("Stream multiple batches", func(t *testing.T) {
		// Подготовка входящего JSON массива
		inputItems := []map[string]string{
			{"correlation_id": "1", "original_url": "https://google.com"},
			{"correlation_id": "2", "original_url": "https://apple.com"},
		}
		jsonBody, _ := json.Marshal(inputItems)

		// Ожидаем, что сервис обработает батч
		// Поскольку в хэндлере используется event.MakeEvent, нам нужно перехватить вызов
		mockService.EXPECT().
			Batch(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, e event.Event) (event.Event, error) {
				// Эмулируем ответ сервиса
				resPayload := event.PayloadBatch{
					{CorrelationID: "1", ShortURL: "http://m.url"},
					{CorrelationID: "2", ShortURL: "http://m.url"},
				}
				return event.MakeEvent(resPayload, e)
			})

		req := httptest.NewRequest(http.MethodPost, "/api/shorten/batch", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HndlAPIShortenBatch()(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		// Проверяем валидность результирующего JSON
		var result []event.PayloadBatchItem
		err := json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err, "Response should be a valid JSON array")
		assert.Len(t, result, 2)
		assert.Equal(t, "http://m.url", result[0].ShortURL)
	})
}

func TestHandlers_HndlGetURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockMicroURLService(ctrl)
	h := NewHandlers(&mockHandlersConfig{}, mockService)

	t.Run("Redirect success", func(t *testing.T) {
		shortPath := "/1_100"
		targetURL := "https://target.com"

		mockService.EXPECT().
			GetURL(gomock.Any(), shortPath).
			Return(targetURL, nil)

		req := httptest.NewRequest(http.MethodGet, shortPath, nil)
		w := httptest.NewRecorder()

		h.HndlGetURL()(w, req)

		assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
		assert.Equal(t, targetURL, w.Header().Get("Location"))
	})
}

func TestHandlers_HndlPing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := NewMockMicroURLService(ctrl)
	h := NewHandlers(&mockHandlersConfig{}, mockService)

	t.Run("DB Up", func(t *testing.T) {
		mockService.EXPECT().Ping(gomock.Any()).Return(nil)

		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		w := httptest.NewRecorder()

		h.HndlPing()(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("DB Down", func(t *testing.T) {
		mockService.EXPECT().Ping(gomock.Any()).Return(errors.New("conn refuse"))

		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		w := httptest.NewRecorder()

		h.HndlPing()(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
