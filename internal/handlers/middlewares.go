package handlers

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Делаем враппер для ResponseWriter
type wrapResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// Перехват StatusCode
func (r *wrapResponseWriter) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// WithLogging добавляет дополнительный код для регистрации сведений о запросе
// и возвращает новый http.Handler.
func WithLogging(cfg IServeConfig, h http.Handler) http.Handler {
	logFn := func(w http.ResponseWriter, r *http.Request) {
		// функция Now() возвращает текущее время
		start := time.Now()

		wrapper := &wrapResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// точка, где выполняется хендлер pingHandler
		h.ServeHTTP(wrapper, r) // обслуживание оригинального запроса

		// отправляем сведения о запросе в zap
		cfg.Zap().Info("http request handled",
			zap.String("uri", r.RequestURI),
			zap.String("method", r.Method),
			zap.Int("status", wrapper.statusCode),
			zap.Duration("duration", time.Since(start)),
		)
	}
	// возвращаем функционально расширенный хендлер
	return http.HandlerFunc(logFn)
}
