package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Mock для конфига
type mockCfg struct{}

func TestWithLogging(t *testing.T) {
	// Настраиваем slog на запись в буфер для проверки вывода
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)

	t.Run("Success request with metrics", func(t *testing.T) {
		buf.Reset()

		// Хендлер, который пишет данные и меняет статус
		content := "Hello, World!"
		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Проверяем наличие метрик в контексте
			m, ok := r.Context().Value(ctxMetricsKey).(*Metrics)
			assert.True(t, ok)
			m.OriginalSize = 500 // Имитируем ручную установку размера

			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(content))
		})

		// Оборачиваем
		middleware := WithLogging(mockCfg{})
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test-url", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// Проверки HTTP ответа
		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, content, rec.Body.String())

		// Проверки логов
		logOutput := buf.String()
		assert.Contains(t, logOutput, "/test-url")
		assert.Contains(t, logOutput, "GET")
		assert.Contains(t, logOutput, "201")
		assert.Contains(t, logOutput, "text/plain")
		assert.Contains(t, logOutput, `"OrigSize":500`)
		assert.Contains(t, logOutput, `"RespSize":13`) // len("Hello, World!")
	})

	t.Run("Default status code", func(t *testing.T) {
		buf.Reset()

		// Хендлер, который ничего не вызывает (по умолчанию 200 OK)
		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

		handler := WithLogging(mockCfg{})(nextHandler)
		req := httptest.NewRequest(http.MethodPost, "/default", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, buf.String(), `"status":200`)
	})

	t.Run("Write with nil metrics safety", func(t *testing.T) {
		// Тестируем напрямую структуру враппера на случай nil метрик (защита кода)
		rec := httptest.NewRecorder()
		wrapper := &wrapResponseWriter{
			ResponseWriter: rec,
			metrics:        nil,
		}

		n, err := wrapper.Write([]byte("test"))
		assert.NoError(t, err)
		assert.Equal(t, 4, n)
		assert.Equal(t, "test", rec.Body.String())
	})
}
