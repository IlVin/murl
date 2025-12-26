package handlers

import (
	"net/http"
)

// Объявляем список используемых параметров конфига
type RouterConfig interface {
}

type ServeConfig interface {
	Listen() string
}

//func NewRouter(h *handlers) *http.ServeMux {
//	mux := http.NewServeMux()
//	mux.HandleFunc("", h.GetFB())
//}

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
func NewRouter(cfg RouterConfig, s MicroURLHandlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{$}", s.HndlAddURL())
	mux.HandleFunc("GET /{id}", s.HndlGetURL())
	mux.HandleFunc("/", s.HndlDefault())

	return mux
}

// Запуск сервера. Передаем конфиг и роутер
func Serve(cfg ServeConfig, router *http.ServeMux) error {
	return http.ListenAndServe(cfg.Listen(), router)
}
