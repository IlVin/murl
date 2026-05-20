package handlers

import (
	"murl/internal/config"
	"murl/internal/handlers/middleware"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHandlers реализует интерфейс MicroURLHandlers для тестирования роутера
type mockHandlers struct {
	called bool
}

func (m *mockHandlers) Ping() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) APIShorten() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) AddURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) GetURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) Default() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) APIShortenBatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) APIUserURLs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) DeleteAPIUserURLs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}

// mockServeCfg для теста функции Serve
type mockServeCfg struct {
	addr         string
	keyFile      string
	certFile     string
	enabledHTTPS bool
}

func (m mockServeCfg) ListenAddr() string { return m.addr }
func (m mockServeCfg) KeyFile() string    { return m.keyFile }
func (m mockServeCfg) CertFile() string   { return m.certFile }
func (m mockServeCfg) EnabledHTTPS() bool { return m.enabledHTTPS }

func TestNewRouter(t *testing.T) {
	// Используем NewConfig для создания базового конфига
	baseCfg, err := config.NewConfig(nil, nil)
	require.NoError(t, err)

	h := &mockHandlers{}

	t.Run("create chi router", func(t *testing.T) {
		cfg := baseCfg.SetRouterType("chi")
		r, err := NewRouter(cfg, h)
		assert.NoError(t, err)
		assert.NotNil(t, r)
	})

	t.Run("create mux router", func(t *testing.T) {
		cfg := baseCfg.SetRouterType("mux")
		r, err := NewRouter(cfg, h)
		assert.NoError(t, err)
		assert.NotNil(t, r)
	})

	t.Run("default to mux", func(t *testing.T) {
		cfg := baseCfg.SetRouterType("unknown")
		r, err := NewRouter(cfg, h)
		assert.NoError(t, err)
		assert.NotNil(t, r)
	})
}

func TestServe(t *testing.T) {
	// Тестируем запуск сервера на случайном порту (порт :0)
	cfg := mockServeCfg{addr: "127.0.0.1:0"}
	mux := http.NewServeMux()

	// Запускаем сервер в горутине, так как ListenAndServe блокирует поток
	go func() {
		err := Serve(cfg, mux)
		if err != nil && err != http.ErrServerClosed {
			return
		}
	}()

	// Даем серверу немного времени на старт
	time.Sleep(50 * time.Millisecond)
}

func TestWithLoggingIntegration(t *testing.T) {
	cfg, _ := config.NewConfig(nil, nil)

	// Простейший хендлер
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mwLogging, err := middleware.WithLogging(cfg)
	assert.NoError(t, err)
	handler := mwLogging(next)
	assert.NotNil(t, handler)
}
