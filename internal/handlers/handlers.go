package handlers

import (
	"io"
	"murl/internal/config"
	"net/http"

	"go.uber.org/zap"
)

// Объявляем список используемых параметров конфига
type IHandlersConfig interface {
	config.IZapLogger
}

// Эти методы сервиса используются хэндлерами
type IMicroURLService interface {
	AddURL(url string) (string, error)
	GetURL(url string) (string, error)
}

type Handlers struct {
	zap     *zap.Logger
	service IMicroURLService
}

func NewHandlers(cfg IHandlersConfig, service IMicroURLService) *Handlers {
	return &Handlers{
		zap:     cfg.Zap(),
		service: service,
	}
}

func (h *Handlers) HndlAddURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				h.zap.Fatal("cannot close r.Body",
					zap.Error(err),
				)
			}
		}()
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			h.zap.Error("cannot read Body",
				zap.Error(err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		murl, err := h.service.AddURL(string(buf))

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Add("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write([]byte(murl)); err != nil {
			h.zap.Debug("failed to write response",
				zap.String("event", "network_error"),
				zap.Error(err),
			)
		}
	}
}

func (h *Handlers) HndlGetURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				h.zap.Fatal("cannot close r.Body",
					zap.Error(err),
				)
			}
		}()
		u, err := h.service.GetURL(r.URL.String())
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
		defer func() {
			if err := r.Body.Close(); err != nil {
				h.zap.Fatal("cannot close r.Body",
					zap.Error(err),
				)
			}
		}()
		w.WriteHeader(http.StatusBadRequest)
	}
}
