package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// LoggingConfig определяет интерфейс конфигурации для логирования.
// На текущий момент интерфейс пуст, но зарезервирован для будущих настроек фильтрации логов.
type LoggingConfig any

// Metrics представляет собой контейнер для сбора количественных показателей обработки запроса.
// Используется для передачи данных между middleware (например, от сжатия к логированию).
type Metrics struct {
	IsCompressed bool
	OriginalSize int64
	ResponseSize int64
}

// wrapResponseWriter расширяет стандартный http.ResponseWriter для перехвата
// статус-кода и подсчета объема переданных байтов.
type wrapResponseWriter struct {
	http.ResponseWriter
	statusCode int
	metrics    *Metrics
}

// Metrics возвращает указатель на структуру с метриками текущего запроса.
func (r *wrapResponseWriter) Metrics() *Metrics {
	return r.metrics
}

// Metrics возвращает указатель на структуру с метриками текущего запроса.
func (r *wrapResponseWriter) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Write записывает данные в ответ и инкрементирует счетчик ResponseSize в метриках.
func (r *wrapResponseWriter) Write(data []byte) (int, error) {
	n, err := r.ResponseWriter.Write(data)

	if m := r.Metrics(); m != nil {
		m.ResponseSize += int64(n)
	}

	return n, err
}

type metricsKey struct{}

// ctxMetricsKey — ключ контекста для хранения и извлечения объекта Metrics.
var ctxMetricsKey = metricsKey{}

// WithLogging возвращает Middleware для детального логирования HTTP-транзакций.
//
// Возможности:
// 1. Измеряет время обработки запроса (latency).
// 2. Логирует URI, метод, статус-код и Content-Type ответа.
// 3. Собирает информацию о размере переданных данных (включая сжатие).
// 4. Интегрируется со стандартным логгером slog.
func WithLogging(cfg LoggingConfig) (func(h http.Handler) http.Handler, error) {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			// отправляем сведения о запросе в slog
			slog.Info("http request handled",
				slog.String("uri", r.RequestURI),
				slog.String("method", r.Method),
				slog.Int("status", wrapper.statusCode),
				slog.String("Content-Type", wrapper.Header().Get("Content-Type")),
				slog.Int64("OrigSize", metrics.OriginalSize),
				slog.Int64("RespSize", metrics.ResponseSize),
				slog.Duration("duration", time.Since(start)),
			)

		})
	}, nil
}
