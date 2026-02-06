package handlers

import (
	"log/slog"
	"murl/internal/handlers/middleware"
	"net/http"

	chi "github.com/go-chi/chi/v5"
)

// Объявляем список используемых параметров конфига
type RouterConfig interface {
	middleware.CompressConfig
	middleware.LoggingConfig
	RouterType() string
}

type IServeConfig interface {
	ListenAddr() string
}

// Интерфейс, в котором описаны методы объекта, необходимые
// для конфигурирования Router'а
type MicroURLHandlers interface {
	HndlAPIShorten() http.HandlerFunc
	HndlAddURL() http.HandlerFunc
	HndlGetURL() http.HandlerFunc
	HndlDefault() http.HandlerFunc
}

// Фабрика роутеров
// Возвращает роутер, заданный в конфиге
func NewRouter(cfg RouterConfig, s MicroURLHandlers) http.Handler {
	switch cfg.RouterType() {
	case "mux":
		return newMuxRouter(cfg, s)
	case "chi":
		return newChiRouter(cfg, s)
	}
	return newMuxRouter(cfg, s)
}

// Возвращает настроенный ServeMux
// Для настройки нужен объект сервиса, который имеет известные методы.
// А список известных методов описан в интерфейсе MicroURLHandlers
// Зачем вообще нужно в роутер передавать объект с известными методами?
// А затем, чтобы в тестах можно было подменить этот объект моком.
func newMuxRouter(cfg RouterConfig, s MicroURLHandlers) http.Handler {
	slog.Info("Used http.ServeMux router")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", s.HndlAPIShorten())
	mux.HandleFunc("POST /{$}", s.HndlAddURL())
	mux.HandleFunc("GET /{id}", s.HndlGetURL())
	mux.HandleFunc("/", s.HndlDefault())

	// Middlewares
	return middleware.WithLogging(cfg)(
		middleware.WithCompress(cfg)(
			mux,
		),
	)
}

// Возвращает настроенный chi.Router
func newChiRouter(cfg RouterConfig, s MicroURLHandlers) http.Handler {
	slog.Info("Used chi router")
	r := chi.NewRouter()

	// Middlewares
	r.Use(middleware.WithLogging(cfg))
	r.Use(middleware.WithCompress(cfg))

	// Routes
	r.Post("/api/shorten", s.HndlAPIShorten())
	r.Post("/", s.HndlAddURL())
	r.Get("/{id}", s.HndlGetURL())
	r.NotFound(s.HndlDefault())
	r.MethodNotAllowed(s.HndlDefault())

	return r
}

// Запуск сервера. Передаем конфиг и роутер
func Serve(cfg IServeConfig, router http.Handler) error {
	slog.Info("Server started")
	return http.ListenAndServe(cfg.ListenAddr(), router)
}
