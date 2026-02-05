package middleware

import (
	"context"
	"murl/internal/config"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type LoggingConfig interface {
	config.ZapLogger
}

// Структура-контейнер для сбора данных
type Metrics struct {
	IsCompressed bool
	OriginalSize int64
	ResponseSize int64
}

// Делаем враппер для ResponseWriter
type wrapResponseWriter struct {
	http.ResponseWriter
	statusCode int
	metrics    *Metrics
}

func (r *wrapResponseWriter) Metrics() *Metrics {
	return r.metrics
}

// Перехват StatusCode
func (r *wrapResponseWriter) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Попсчет размера ответа
func (r *wrapResponseWriter) Write(data []byte) (int, error) {
	n, err := r.ResponseWriter.Write(data)

	if m := r.Metrics(); m != nil {
		m.ResponseSize += int64(n)
	}

	return n, err
}

type metricsKey struct{}

var ctxMetricsKey = metricsKey{}

// WithLogging добавляет дополнительный код для регистрации сведений о запросе
// и возвращает новый http.Handler.
func WithLogging(cfg LoggingConfig) func(h http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		logFn := func(w http.ResponseWriter, r *http.Request) {
			// функция Now() возвращает текущее время
			start := time.Now()

			// Создаем контекст для метрик
			metrics := &Metrics{}
			ctx := context.WithValue(r.Context(), ctxMetricsKey, metrics)

			wrapper := &wrapResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				metrics:        metrics,
			}

			// Передаем запрос дальше с новым контекстом
			h.ServeHTTP(wrapper, r.WithContext(ctx))

			// отправляем сведения о запросе в zap
			cfg.Zap().Info("http request handled",
				zap.String("uri", r.RequestURI),
				zap.String("method", r.Method),
				zap.Int("status", wrapper.statusCode),
				zap.String("Content-Type", wrapper.Header().Get("Content-Type")),
				zap.Int64("OrigSize", metrics.OriginalSize),
				zap.Int64("RespSize", metrics.ResponseSize),
				zap.Duration("duration", time.Since(start)),
			)

		}
		// возвращаем функционально расширенный хендлер
		return http.HandlerFunc(logFn)
	}
}
