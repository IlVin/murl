package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Вспомогательные структуры для моков (если не используете mockgen) ---

type MockService struct {
	AddURLFunc func(ctx context.Context, url string) (string, error)
	GetURLFunc func(ctx context.Context, url string) (string, error)
	PingFunc   func(ctx context.Context) error
}

func (m *MockService) AddURL(ctx context.Context, url string) (string, error) {
	return m.AddURLFunc(ctx, url)
}
func (m *MockService) GetURL(ctx context.Context, url string) (string, error) {
	return m.GetURLFunc(ctx, url)
}
func (m *MockService) Ping(ctx context.Context) error { return m.PingFunc(ctx) }

type MockConfig struct{}

// --- ТЕСТЫ ---

func TestHandlers_HndlAddURL(t *testing.T) {
	svc := &MockService{}
	h := NewHandlers(&MockConfig{}, svc)

	t.Run("success", func(t *testing.T) {
		svc.AddURLFunc = func(ctx context.Context, url string) (string, error) {
			return "http://short/1", nil
		}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("http://long.com"))
		rec := httptest.NewRecorder()

		h.HndlAddURL()(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
		assert.Equal(t, "http://short/1", rec.Body.String())
	})

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
		rec := httptest.NewRecorder()

		h.HndlAddURL()(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("body too large", func(t *testing.T) {
		largeData := make([]byte, bodyLimit+2)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(largeData))
		rec := httptest.NewRecorder()

		h.HndlAddURL()(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("service error", func(t *testing.T) {
		svc.AddURLFunc = func(ctx context.Context, url string) (string, error) {
			return "", errors.New("db error")
		}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("http://long.com"))
		rec := httptest.NewRecorder()

		h.HndlAddURL()(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestHandlers_HndlAPIShorten(t *testing.T) {
	svc := &MockService{}
	h := NewHandlers(&MockConfig{}, svc)

	t.Run("success", func(t *testing.T) {
		svc.AddURLFunc = func(ctx context.Context, url string) (string, error) {
			return "http://short/1", nil
		}
		body := `{"url": "http://long.com"}`
		req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		rec := httptest.NewRecorder()

		h.HndlAPIShorten()(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Contains(t, rec.Body.String(), `"result":"http://short/1"`)
	})

	t.Run("invalid content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/shorten", nil)
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()

		h.HndlAPIShorten()(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{invalid`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.HndlAPIShorten()(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("service internal error", func(t *testing.T) {
		svc.AddURLFunc = func(ctx context.Context, url string) (string, error) {
			return "", errors.New("internal fail")
		}
		req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(`{"url":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.HndlAPIShorten()(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandlers_HndlGetURL(t *testing.T) {
	svc := &MockService{}
	h := NewHandlers(&MockConfig{}, svc)

	t.Run("redirect success", func(t *testing.T) {
		svc.GetURLFunc = func(ctx context.Context, url string) (string, error) {
			return "http://long.com", nil
		}
		req := httptest.NewRequest(http.MethodGet, "/abc", nil)
		rec := httptest.NewRecorder()

		h.HndlGetURL()(rec, req)

		assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
		assert.Equal(t, "http://long.com", rec.Header().Get("Location"))
	})

	t.Run("not found", func(t *testing.T) {
		svc.GetURLFunc = func(ctx context.Context, url string) (string, error) {
			return "", errors.New("not found")
		}
		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		rec := httptest.NewRecorder()

		h.HndlGetURL()(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestHandlers_HndlPing(t *testing.T) {
	svc := &MockService{}
	h := NewHandlers(&MockConfig{}, svc)

	t.Run("ping success", func(t *testing.T) {
		svc.PingFunc = func(ctx context.Context) error { return nil }
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		rec := httptest.NewRecorder()

		h.HndlPing()(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("ping failure", func(t *testing.T) {
		svc.PingFunc = func(ctx context.Context) error { return errors.New("db down") }
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		rec := httptest.NewRecorder()

		h.HndlPing()(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestHandlers_HndlDefault(t *testing.T) {
	h := NewHandlers(&MockConfig{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/what", nil)
	rec := httptest.NewRecorder()

	h.HndlDefault()(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Тест на ошибку чтения (имитация разрыва соединения)
type ErrorReader struct{}

func (e *ErrorReader) Read(p []byte) (n int, err error) { return 0, fmt.Errorf("read error") }
func (e *ErrorReader) Close() error                     { return nil }

func TestHandlers_ReadBodyError(t *testing.T) {
	h := NewHandlers(&MockConfig{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/", &ErrorReader{})
	rec := httptest.NewRecorder()

	h.HndlAddURL()(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
