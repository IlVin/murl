package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// mockLogConfig реализует IServeConfig для теста логов
type mockLogConfig struct {
	logger *zap.Logger
}

func (m mockLogConfig) Zap() *zap.Logger   { return m.logger }
func (m mockLogConfig) ListenAddr() string { return "" }

func TestWithLogging(t *testing.T) {
	t.Run("log status 200 by default", func(t *testing.T) {
		core, obs := observer.New(zap.InfoLevel)
		logger := zap.New(core)
		cfg := mockLogConfig{logger: logger}

		// Хендлер, который ничего не пишет в заголовок
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := w.Write([]byte("ok")); err != nil {
				logger.Debug("failed to write response",
					zap.String("event", "network_error"),
					zap.Error(err),
				)
			}
		})

		handler := WithLogging(cfg, next)

		req := httptest.NewRequest(http.MethodGet, "/test-200", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		// Проверяем логи
		assert.Equal(t, 1, obs.Len())
		logEntry := obs.All()[0]
		assert.Equal(t, "http request handled", logEntry.Message)

		// Проверяем поля в логе
		fields := logEntry.ContextMap()
		assert.Equal(t, "/test-200", fields["uri"])
		assert.Equal(t, "GET", fields["method"])
		assert.Equal(t, int64(200), fields["status"]) // По умолчанию 200
	})

	t.Run("log explicit status 400", func(t *testing.T) {
		core, obs := observer.New(zap.InfoLevel)
		logger := zap.New(core)
		cfg := mockLogConfig{logger: logger}

		// Хендлер, который явно ставит 400
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		})

		handler := WithLogging(cfg, next)

		req := httptest.NewRequest(http.MethodPost, "/test-400", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		logEntry := obs.All()[0]
		fields := logEntry.ContextMap()
		assert.Equal(t, int64(400), fields["status"])
		assert.Equal(t, "POST", fields["method"])
	})
}
