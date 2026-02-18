package middleware

import (
	"net/http"
)

type LimiterConfig interface {
	MaxBodySize() int64
}

// WithLimiter добавляет дополнительный код для ограничения длины запроса.
func WithLimiter(cfg LimiterConfig) func(h http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodySize())
			// Передаем запрос дальше
			h.ServeHTTP(w, r)
		})
	}
}
