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
	middleware.LimiterConfig
	middleware.SessionConfig
	RouterType() string
}

type IServeConfig interface {
	ListenAddr() string
}

// Интерфейс, в котором описаны методы объекта, необходимые
// для конфигурирования Router'а
type MicroURLHandlers interface {
	APIShorten() http.HandlerFunc
	APIShortenBatch() http.HandlerFunc
	APIUserURLs() http.HandlerFunc
	AddURL() http.HandlerFunc
	GetURL() http.HandlerFunc
	Default() http.HandlerFunc
	Ping() http.HandlerFunc
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
	mux.HandleFunc("POST /api/shorten", s.APIShorten())
	mux.HandleFunc("POST /api/shorten/batch", s.APIShortenBatch())
	mux.HandleFunc("GET /api/user/urls", s.APIUserURLs())
	mux.HandleFunc("POST /{$}", s.AddURL())
	mux.HandleFunc("GET /ping", s.Ping())
	mux.HandleFunc("GET /{id}", s.GetURL())
	mux.HandleFunc("/", s.Default())

	// Middlewares
	return middleware.WithLimiter(cfg)(
		middleware.WithSession(cfg)(
			middleware.WithLogging(cfg)(
				middleware.WithCompress(cfg)(
					mux,
				),
			),
		),
	)
}

// Возвращает настроенный chi.Router
func newChiRouter(cfg RouterConfig, s MicroURLHandlers) http.Handler {
	slog.Info("Used chi router")
	r := chi.NewRouter()

	// Middlewares
	r.Use(middleware.WithLimiter(cfg))
	r.Use(middleware.WithSession(cfg))
	r.Use(middleware.WithLogging(cfg))
	r.Use(middleware.WithCompress(cfg))

	// Routes
	r.Post("/api/shorten", s.APIShorten())
	r.Post("/api/shorten/batch", s.APIShortenBatch())
	r.Get("/api/user/urls", s.APIUserURLs())
	r.Post("/", s.AddURL())
	r.Get("/ping", s.Ping())
	r.Get("/{id}", s.GetURL())
	r.NotFound(s.Default())
	r.MethodNotAllowed(s.Default())

	return r
}

// Запуск сервера. Передаем конфиг и роутер
func Serve(cfg IServeConfig, router http.Handler) error {
	slog.Info("Server started")
	return http.ListenAndServe(cfg.ListenAddr(), router)
}
