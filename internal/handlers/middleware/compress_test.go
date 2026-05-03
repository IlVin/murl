package middleware

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/stretchr/testify/assert"
)

// Мок конфига
type mockConfig struct {
	types map[string]struct{}
}

func (m *mockConfig) CompressibleContentTypes() map[string]struct{} { return m.types }

func TestWithCompress(t *testing.T) {
	compressibleTypes := map[string]struct{}{
		"text/html":        {},
		"application/json": {},
	}
	cfg := &mockConfig{types: compressibleTypes}

	// Тестовый хендлер, который пишет данные
	content := "hello compression world, hello compression world"
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	})

	mw, err := WithCompress(cfg)
	assert.NoError(t, err)
	handler := mw(nextHandler)

	tests := []struct {
		name           string
		acceptEncoding string
		expectedEnc    string
	}{
		{"Gzip", "gzip", "gzip"},
		{"Brotli", "br", "br"},
		{"Deflate", "deflate", "deflate"},
		{"Multiple with q", "gzip;q=0.5, br;q=1.0", "br"},
		{"Identity/None", "", ""},
		{"Not supported", "rar, zip", ""},
		{"Q zero", "gzip;q=0", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.acceptEncoding != "" {
				r.Header.Set("Accept-Encoding", tt.acceptEncoding)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, r)

			assert.Equal(t, tt.expectedEnc, w.Header().Get("Content-Encoding"))
			assert.Contains(t, w.Header().Get("Vary"), "Accept-Encoding")

			// Проверка корректности распаковки
			var decodedBody []byte
			switch tt.expectedEnc {
			case "gzip":
				gr, _ := gzip.NewReader(w.Body)
				decodedBody, _ = io.ReadAll(gr)
			case "br":
				br := brotli.NewReader(w.Body)
				decodedBody, _ = io.ReadAll(br)
			case "deflate":
				fr := flate.NewReader(w.Body)
				decodedBody, _ = io.ReadAll(fr)
			default:
				decodedBody = w.Body.Bytes()
			}
			assert.Equal(t, content, string(decodedBody))
		})
	}
}

func TestCompress_EdgeCases(t *testing.T) {
	cfg := &mockConfig{types: map[string]struct{}{"text/html": {}}}

	t.Run("Already compressed", func(t *testing.T) {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Encoding", "custom")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("data")); err != nil {
				slog.Error("write data fail",
					slog.Any("err", err),
				)
			}
		})
		mw, err := WithCompress(cfg)
		assert.NoError(t, err)
		handler := mw(next)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", "gzip")

		handler.ServeHTTP(w, r)
		assert.Equal(t, "custom", w.Header().Get("Content-Encoding"), "Should not re-compress")
	})

	t.Run("Non-compressible type", func(t *testing.T) {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("png-data")); err != nil {
				slog.Error("write png-data fail",
					slog.Any("err", err),
				)
			}
		})
		mw, err := WithCompress(cfg)
		assert.NoError(t, err)
		handler := mw(next)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", "gzip")

		handler.ServeHTTP(w, r)
		assert.Empty(t, w.Header().Get("Content-Encoding"))
	})

	t.Run("Invalid Q Weight", func(t *testing.T) {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if _, err := w.Write([]byte("data")); err != nil {
				slog.Error("write data fail",
					slog.Any("err", err),
				)
			}

		})
		mw, err := WithCompress(cfg)
		assert.NoError(t, err)
		handler := mw(next)

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", "gzip;q=invalid")

		handler.ServeHTTP(w, r)
		assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"), "Should fallback to q=1.0")
	})
}

func TestCompress_Metrics(t *testing.T) {
	cfg := &mockConfig{types: map[string]struct{}{"text/html": {}}}
	metrics := &Metrics{}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte("some data to compress")); err != nil {
			slog.Error("write data fail",
				slog.Any("err", err),
			)
		}

	})

	mw, err := WithCompress(cfg)
	assert.NoError(t, err)
	handler := mw(next)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	// Кладем метрики в контекст, как это делает WithLogging
	ctx := context.WithValue(r.Context(), ctxMetricsKey, metrics)
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	assert.True(t, metrics.IsCompressed)
	assert.Greater(t, metrics.OriginalSize, int64(0))
}

func TestWriteHeader_DoubleCall(t *testing.T) {
	// Тест покрытия headerWritten
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	cw := NewCompressResponseWriter(w, r, nil, nil)

	cw.WriteHeader(http.StatusOK)
	cw.WriteHeader(http.StatusNotFound) // Второй вызов

	assert.Equal(t, http.StatusOK, w.Code)
}
