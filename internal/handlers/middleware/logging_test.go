package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// Мок конфигурации
type mockLogConfig struct {
	logger *zap.Logger
}

func (m *mockLogConfig) Zap() *zap.Logger {
	return m.logger
}

func TestWithLogging(t *testing.T) {
	// 1. Настраиваем перехват логов zap
	core, obs := observer.New(zap.InfoLevel)
	logger := zap.New(core)
	cfg := &mockLogConfig{logger: logger}

	t.Run("successful request with custom status and metrics", func(t *testing.T) {
		content := "test data"
		handlerToTest := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Проверяем, что метрики доступны в контексте
			m, ok := r.Context().Value(ctxMetricsKey).(*TMetrics)
			assert.True(t, ok)

			// Эмулируем работу другого middleware (например, сжатия)
			m.OriginalSize = 100
			m.IsCompressed = true

			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(content))
		})

		h := WithLogging(cfg, handlerToTest)

		r := httptest.NewRequest(http.MethodGet, "/test-uri", nil)
		w := httptest.NewRecorder()

		h.ServeHTTP(w, r)

		// Проверки ответа
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, content, w.Body.String())

		// Проверка логов
		logs := obs.All()
		assert.Len(t, logs, 1)

		fields := logs[0].ContextMap()
		fmt.Printf("%v", fields)
		assert.Equal(t, "/test-uri", fields["uri"])
		assert.Equal(t, "GET", fields["method"])
		assert.Equal(t, int64(http.StatusCreated), fields["status"])
		assert.Equal(t, "text/plain", fields["Content-Type"])
		assert.Equal(t, int64(100), fields["OrigSize"])
		assert.Equal(t, int64(len(content)), fields["RespSize"])
		assert.NotNil(t, fields["duration"])
	})

	t.Run("default status code", func(t *testing.T) {
		handlerToTest := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WriteHeader не вызывается, должен быть 200 OK
			_, _ = w.Write([]byte("ok"))
		})

		h := WithLogging(cfg, handlerToTest)
		r := httptest.NewRequest(http.MethodPost, "/default", nil)
		w := httptest.NewRecorder()

		h.ServeHTTP(w, r)

		logs := obs.All()
		// Берем последний лог (второй в этом тесте)
		lastLog := logs[len(logs)-1]
		assert.Equal(t, int64(http.StatusOK), lastLog.ContextMap()["status"])
	})
}

func TestWrapResponseWriter_NoMetrics(t *testing.T) {
	// Тест для покрытия случая, если метрики вдруг nil (защита от паники)
	w := httptest.NewRecorder()
	wrapper := &wrapResponseWriter{
		ResponseWriter: w,
		metrics:        nil,
	}

	n, err := wrapper.Write([]byte("data"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Nil(t, wrapper.Metrics())
}
