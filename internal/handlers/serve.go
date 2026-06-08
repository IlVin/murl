package handlers

import (
	"context"
	"log/slog"
	"murl/internal/handlers/middleware"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	chi "github.com/go-chi/chi/v5"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"

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
func Serve(cfg IServeConfig, router http.Handler, grpcServer *grpc.Server) error {
	// 1. Создаем один общий TCP-listener
	lis, err := net.Listen("tcp", cfg.ListenAddr())
	if err != nil {
		return err
	}

	// 2. Создаем cmux инстанс поверх listener
	m := cmux.New(lis)

	// 3. Объявляем правила сортировки трафика
	// gRPC всегда использует HTTP/2 с определенным заголовком (обычно proto-запросы начинаются с этого)
	grpcLis := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	// Все остальное отдаем под обычный HTTP
	httpLis := m.Match(cmux.Any())

	// 4. Инициализируем стандартный HTTP-сервер
	srv := http.Server{
		Handler: router,
	}

	// Канал для отслеживания ошибок запуска горутин
	errChan := make(chan error, 3)

	// Запуск gRPC
	go func() {
		slog.Info("gRPC sub-server starting")
		if err := grpcServer.Serve(grpcLis); err != nil && err != grpc.ErrServerStopped {
			errChan <- err
		}
	}()

	// Запуск HTTP (или HTTPS)
	go func() {
		slog.Info("HTTP sub-server starting")
		var err error
		if cfg.EnabledHTTPS() {
			err = srv.ServeTLS(httpLis, cfg.CertFile(), cfg.KeyFile())
		} else {
			err = srv.Serve(httpLis)
		}
		if err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Запуск самого мультиплексора (это блокирующий вызов)
	go func() {
		slog.Info("Multiplexer (cmux) started", "addr", cfg.ListenAddr())
		if err := m.Serve(); err != nil {
			errChan <- err
		}
	}()

	// Ожидание сигналов закрытия
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errChan:
		slog.Error("Server crashed", "error", err)
	case <-sigChan:
		slog.Info("Shutting down servers...")
	}

	// Грациозное завершение работы
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Стопаем gRPC и HTTP
	grpcServer.GracefulStop()
	return srv.Shutdown(ctx)
}
