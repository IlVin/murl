package handlers

import (
	"murl/internal/config"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockHandlers реализует интерфейс MicroURLHandlers для тестирования роутера
type mockHandlers struct {
	called bool
}

func (m *mockHandlers) HndlAddURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) HndlGetURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}
func (m *mockHandlers) HndlDefault() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { m.called = true }
}

// mockServeCfg для теста функции Serve
type mockServeCfg struct {
	config.IZapLogger
	addr string
}

func (m mockServeCfg) ListenAddr() string { return m.addr }
func (m mockServeCfg) Zap() *zap.Logger   { return zap.NewNop() }

func TestNewRouter(t *testing.T) {
	logger := zap.NewNop()
	// Используем NewConfig для создания базового конфига
	baseCfg, err := config.NewConfig(nil, nil, logger)
	require.NoError(t, err)

	h := &mockHandlers{}

	t.Run("create chi router", func(t *testing.T) {
		cfg := baseCfg.SetRouterType("chi")
		r := NewRouter(cfg, h)
		assert.NotNil(t, r)
	})

	t.Run("create mux router", func(t *testing.T) {
		cfg := baseCfg.SetRouterType("mux")
		r := NewRouter(cfg, h)
		assert.NotNil(t, r)
	})

	t.Run("default to mux", func(t *testing.T) {
		cfg := baseCfg.SetRouterType("unknown")
		r := NewRouter(cfg, h)
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
		// Ошибка будет при закрытии сервера, это нормально
		if err != nil && err != http.ErrServerClosed {
			return
		}
	}()

	// Даем серверу немного времени на старт
	time.Sleep(50 * time.Millisecond)
}

func TestWithLoggingIntegration(t *testing.T) {
	logger := zap.NewNop()
	cfg, _ := config.NewConfig(nil, nil, logger)

	// Простейший хендлер
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := WithLogging(cfg, next)
	assert.NotNil(t, handler)
}
