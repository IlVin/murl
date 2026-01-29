package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// mockLogConfig реализует IServeConfig для теста
type mockLogConfig struct {
	logger *zap.Logger
}

func (m mockLogConfig) Zap() *zap.Logger   { return m.logger }
func (m mockLogConfig) ListenAddr() string { return "" }

func TestWithLogging(t *testing.T) {
	t.Run("log status 200 and body size", func(t *testing.T) {
		// Создаем наблюдатель за логами
		core, obs := observer.New(zap.InfoLevel)
		logger := zap.New(core)
		cfg := mockLogConfig{logger: logger}

		content := "hello world"
		// Хендлер, который пишет данные (проверка Write и размера)
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(content))
		})

		handler := WithLogging(cfg, next)

		req := httptest.NewRequest(http.MethodGet, "/test-size", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Проверяем логи
		assert.Equal(t, 1, obs.Len())
		logEntry := obs.All()[0]

		fields := logEntry.ContextMap()
		assert.Equal(t, "/test-size", fields["uri"])
		assert.Equal(t, "GET", fields["method"])
		assert.Equal(t, int64(200), fields["status"])
		assert.Equal(t, int64(len(content)), fields["size"]) // Проверка нового поля
		assert.Contains(t, fields, "duration")
	})

	t.Run("log explicit status 400", func(t *testing.T) {
		core, obs := observer.New(zap.InfoLevel)
		logger := zap.New(core)
		cfg := mockLogConfig{logger: logger}

		// Хендлер, который явно ставит статус (проверка WriteHeader)
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("bad"))
		})

		handler := WithLogging(cfg, next)

		req := httptest.NewRequest(http.MethodPost, "/test-400", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		logEntry := obs.All()[0]
		fields := logEntry.ContextMap()
		assert.Equal(t, int64(400), fields["status"])
		assert.Equal(t, int64(3), fields["size"]) // "bad" = 3 bytes
	})
}
