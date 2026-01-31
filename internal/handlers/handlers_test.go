package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// mockService реализует интерфейс IMicroURLService
type mockService struct {
	mock.Mock
}

func (m *mockService) AddURL(url string) (string, error) {
	args := m.Called(url)
	return args.String(0), args.Error(1)
}

func (m *mockService) GetURL(url string) (string, error) {
	args := m.Called(url)
	return args.String(0), args.Error(1)
}

// mockCfg реализует интерфейс IHandlersConfig
type mockCfg struct {
	logger *zap.Logger
}

func (m mockCfg) Zap() *zap.Logger {
	return m.logger
}

// errReader имитирует ошибку чтения при вызове io.ReadAll
type errReader struct{}

func (e *errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}
func (e *errReader) Close() error { return nil }

func TestHandlers_HndlAPIShorten(t *testing.T) {
	logger := zap.NewNop()
	cfg := mockCfg{logger: logger}

	t.Run("success 201", func(t *testing.T) {
		svc := new(mockService)
		h := NewHandlers(cfg, svc)
		longURL := "https://google.com"
		shortURL := "http://localhost:8080/AAA"

		svc.On("AddURL", longURL).Return(shortURL, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader("{\"url\":\""+longURL+"\"}"))
		req.Header["Content-Type"] = []string{"application/json"}
		w := httptest.NewRecorder()

		h.HndlAPIShorten()(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		assert.Equal(t, "{\"result\":\""+shortURL+"\"}", w.Body.String())
	})

	t.Run("read body error 500", func(t *testing.T) {
		h := NewHandlers(cfg, nil)
		req := httptest.NewRequest(http.MethodPost, "/", &errReader{})
		w := httptest.NewRecorder()

		h.HndlAddURL()(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("service error 400", func(t *testing.T) {
		svc := new(mockService)
		h := NewHandlers(cfg, svc)
		svc.On("AddURL", mock.Anything).Return("", errors.New("invalid url"))

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("bad-url"))
		w := httptest.NewRecorder()

		h.HndlAddURL()(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandlers_HndlAddURL(t *testing.T) {
	logger := zap.NewNop()
	cfg := mockCfg{logger: logger}

	t.Run("success 201", func(t *testing.T) {
		svc := new(mockService)
		h := NewHandlers(cfg, svc)
		longURL := "https://google.com"
		shortURL := "http://localhost:8080/AAA"

		svc.On("AddURL", longURL).Return(shortURL, nil)

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(longURL))
		w := httptest.NewRecorder()

		h.HndlAddURL()(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
		assert.Equal(t, shortURL, w.Body.String())
	})

	t.Run("read body error 500", func(t *testing.T) {
		h := NewHandlers(cfg, nil)
		req := httptest.NewRequest(http.MethodPost, "/", &errReader{})
		w := httptest.NewRecorder()

		h.HndlAddURL()(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("service error 400", func(t *testing.T) {
		svc := new(mockService)
		h := NewHandlers(cfg, svc)
		svc.On("AddURL", mock.Anything).Return("", errors.New("invalid url"))

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("bad-url"))
		w := httptest.NewRecorder()

		h.HndlAddURL()(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandlers_HndlGetURL(t *testing.T) {
	logger := zap.NewNop()
	cfg := mockCfg{logger: logger}

	t.Run("success redirect 307", func(t *testing.T) {
		svc := new(mockService)
		h := NewHandlers(cfg, svc)
		longURL := "https://google.com"

		svc.On("GetURL", "/AAA").Return(longURL, nil)

		req := httptest.NewRequest(http.MethodGet, "/AAA", nil)
		w := httptest.NewRecorder()

		h.HndlGetURL()(w, req)

		assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
		assert.Equal(t, longURL, w.Header().Get("Location"))
	})

	t.Run("not found 400", func(t *testing.T) {
		svc := new(mockService)
		h := NewHandlers(cfg, svc)
		svc.On("GetURL", mock.Anything).Return("", errors.New("not found"))

		req := httptest.NewRequest(http.MethodGet, "/invalid", nil)
		w := httptest.NewRecorder()

		h.HndlGetURL()(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandlers_HndlDefault(t *testing.T) {
	h := NewHandlers(mockCfg{logger: zap.NewNop()}, nil)
	req := httptest.NewRequest(http.MethodPatch, "/", nil)
	w := httptest.NewRecorder()

	h.HndlDefault()(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
