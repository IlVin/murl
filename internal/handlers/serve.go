package handlers

import (
	"net/http"

	chi "github.com/go-chi/chi/v5"
)

// Объявляем список используемых параметров конфига
type RouterConfig interface {
}

type ServeConfig interface {
	Listen() string
}

// Интерфейс, в котором описаны методы объекта, необходимые
// для конфигурирования Router'а
type MicroURLHandlers interface {
	HndlAddURL() http.HandlerFunc
	HndlGetURL() http.HandlerFunc
	HndlDefault() http.HandlerFunc
}

// Возвращает настроенный ServeMux
// Для настройки нужен объект сервиса, который имеет известные методы.
// А список известных методов описан в интерфейсе MicroURLHandlers
// Зачем вообще нужно в роутер передавать объект с известными методами?
// А затем, чтобы в тестах можно было подменить этот объект моком.
func NewMuxRouter(cfg RouterConfig, s MicroURLHandlers) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{$}", s.HndlAddURL())
	mux.HandleFunc("GET /{id}", s.HndlGetURL())
	mux.HandleFunc("/", s.HndlDefault())

	return mux
}

// Возвращает настроенный chi.Router
func NewChiRouter(cfg RouterConfig, s MicroURLHandlers) http.Handler {
	r := chi.NewRouter()
	r.Post("/", s.HndlAddURL())
	r.Get("/{id}", s.HndlGetURL())
	r.NotFound(s.HndlDefault())
	r.MethodNotAllowed(s.HndlDefault())

	return r
}

// Запуск сервера. Передаем конфиг и роутер
func Serve(cfg ServeConfig, router http.Handler) error {
	return http.ListenAndServe(cfg.Listen(), router)
}
