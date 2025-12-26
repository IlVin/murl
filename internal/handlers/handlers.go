package handlers

import (
	"bytes"
	"fmt"
	"net/http"
)

// Объявляем список используемых параметров конфига
type HandlersConfig interface {
}

// Эти методы сервиса используются хэндлерами
type MicroURLService interface {
	AddUrl(url string) (string, error)
	GetUrl(url string) (string, error)
}

type Handlers struct {
	cfg     HandlersConfig
	service MicroURLService
}

func NewHandlers(cfg HandlersConfig, service MicroURLService) *Handlers {
	return &Handlers{
		cfg:     cfg,
		service: service,
	}
}

func (h *Handlers) HndlAddUrl() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		//		w.WriteHeader(http.StatusOK)
		var buf bytes.Buffer
		_, err := buf.ReadFrom(r.Body)
		if err != nil {
			fmt.Println(err)
		}
		murl, err := h.service.AddUrl(buf.String())

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(murl))
	}
}

func (h *Handlers) HndlGetUrl() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		shortId := r.PathValue("id")
		u, err := h.service.GetUrl(shortId)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Add("Location", u)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}
}

func (h *Handlers) HndlDefault() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}
}
