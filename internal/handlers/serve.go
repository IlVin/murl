package handlers

import (
	"log/slog"
	"murl/internal/handlers/middleware"
	"net/http"

	chi "github.com/go-chi/chi/v5"

	_ "net/http/pprof"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=serve_mock_test.go -package=$GOPACKAGE

// RouterConfig определяет набор интерфейсов конфигурации, необходимых для настройки
// роутера и всех подключаемых Middleware (сжатие, логирование, лимитер, сессии).
type RouterConfig interface {
	middleware.CompressConfig
	middleware.LimiterConfig
	middleware.SessionConfig
	// RouterType возвращает идентификатор типа роутера ("chi" или "mux").
	RouterType() string
}

// IServeConfig содержит настройки, необходимые для физического запуска HTTP-сервера.
type IServeConfig interface {
	ListenAddr() string
	EnabledHTTPS() bool
	KeyFile() string
	CertFile() string
}

// MicroURLHandlers описывает контракт объекта обработчиков, необходимых
// для конфигурирования маршрутов. Использование интерфейса позволяет легко
// подменять реализацию хендлеров (например, на моки в тестах).
type MicroURLHandlers interface {
	APIShorten() http.HandlerFunc
	APIShortenBatch() http.HandlerFunc
	APIUserURLs() http.HandlerFunc
	DeleteAPIUserURLs() http.HandlerFunc
	AddURL() http.HandlerFunc
	GetURL() http.HandlerFunc
	Default() http.HandlerFunc
	Ping() http.HandlerFunc
}

// NewRouter — фабрика для создания HTTP-обработчика (роутера).
// Выбирает реализацию ("chi" или стандартный "mux") на основе переданной конфигурации.
func NewRouter(cfg RouterConfig, s MicroURLHandlers) (http.Handler, error) {
	switch cfg.RouterType() {
	case "mux":
		return newMuxRouter(cfg, s)
	case "chi":
		return newChiRouter(cfg, s)
	}
	return newMuxRouter(cfg, s)
}

// newMuxRouter настраивает стандартный http.ServeMux (доступно в Go 1.22+).
// Реализует вложенную структуру Middleware через классическое функциональное оборачивание.
func newMuxRouter(cfg RouterConfig, s MicroURLHandlers) (http.Handler, error) {
	slog.Info("Used http.ServeMux router")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", s.APIShorten())
	mux.HandleFunc("POST /api/shorten/batch", s.APIShortenBatch())
	mux.HandleFunc("GET /api/user/urls", s.APIUserURLs())
	mux.HandleFunc("DELETE /api/user/urls", s.DeleteAPIUserURLs())
	mux.HandleFunc("POST /{$}", s.AddURL())
	mux.HandleFunc("GET /ping", s.Ping())
	mux.HandleFunc("GET /{id}", s.GetURL())
	mux.HandleFunc("/", s.Default())

	// Middlewares
	mwCompress, err := middleware.WithCompress(cfg)
	if err != nil {
		return nil, err
	}
	mws := mwCompress(mux)

	mwLogging, err := middleware.WithLogging(cfg)
	if err != nil {
		return nil, err
	}
	mws = mwLogging(mws)

	mwSession, err := middleware.WithSession(cfg)
	if err != nil {
		return nil, err
	}
	mws = mwSession(mws)

	mwLimiter, err := middleware.WithLimiter(cfg)
	if err != nil {
		return nil, err
	}
	mws = mwLimiter(mws)

	return mws, nil
}

// newChiRouter настраивает роутер на базе библиотеки chi.
// Дополнительно подключает эндпоинты pprof для профилирования приложения.
func newChiRouter(cfg RouterConfig, s MicroURLHandlers) (http.Handler, error) {
	slog.Info("Used chi router")
	r := chi.NewRouter()

	// Middlewares
	mwCompress, err := middleware.WithCompress(cfg)
	if err != nil {
		return nil, err
	}
	r.Use(mwCompress)

	mwLogging, err := middleware.WithLogging(cfg)
	if err != nil {
		return nil, err
	}
	r.Use(mwLogging)

	mwSession, err := middleware.WithSession(cfg)
	if err != nil {
		return nil, err
	}
	r.Use(mwSession)

	mwLimiter, err := middleware.WithLimiter(cfg)
	if err != nil {
		return nil, err
	}
	r.Use(mwLimiter)

	// Подключаем pprof
	r.Mount("/debug/pprof", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.DefaultServeMux.ServeHTTP(w, r)
	}))

	// Routes
	r.Post("/api/shorten", s.APIShorten())
	r.Post("/api/shorten/batch", s.APIShortenBatch())
	r.Get("/api/user/urls", s.APIUserURLs())
	r.Delete("/api/user/urls", s.DeleteAPIUserURLs())
	r.Post("/", s.AddURL())
	r.Get("/ping", s.Ping())
	r.Get("/{id}", s.GetURL())
	r.NotFound(s.Default())
	r.MethodNotAllowed(s.Default())

	return r, nil
}

// Serve выполняет запуск HTTP-сервера на указанном в конфигурации адресе.
// Метод является блокирующим и возвращает ошибку, если сервер не смог запуститься
// или прекратил работу аварийно.
func Serve(cfg IServeConfig, router http.Handler) error {

	slog.Info("Server started")
	if cfg.EnabledHTTPS() {
		return http.ListenAndServeTLS(cfg.ListenAddr(), cfg.CertFile(), cfg.KeyFile(), router)
	}
	return http.ListenAndServe(cfg.ListenAddr(), router)
}
