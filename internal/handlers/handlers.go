package handlers

import (
	"encoding/json"
	"io"
	"murl/internal/config"
	"net/http"

	"go.uber.org/zap"
)

// Объявляем список используемых параметров конфига
type HandlersConfig interface {
	config.ZapLogger
}

// Эти методы сервиса используются хэндлерами
type MicroURLService interface {
	AddURL(url string) (string, error)
	GetURL(url string) (string, error)
}

type Handlers struct {
	zap     *zap.Logger
	service MicroURLService
}

func NewHandlers(cfg HandlersConfig, service MicroURLService) *Handlers {
	return &Handlers{
		zap:     cfg.Zap(),
		service: service,
	}
}

// =========== POST / ==================
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

// =========== POST /api/shorten ==================
type APIShortenReq struct {
	URL string `json:"url"`
}

type TAPIShortenResp struct {
	Result string `json:"result"`
}

func (h *Handlers) HndlAPIShorten() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				h.zap.Fatal("cannot close r.Body",
					zap.Error(err),
				)
			}
		}()

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			h.zap.Warn("invalid Content-Type",
				zap.String("Content-Type", ct),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		buf, err := io.ReadAll(r.Body)
		if err != nil {
			h.zap.Error("cannot read Body",
				zap.Error(err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var jsReq APIShortenReq
		var jsResp TAPIShortenResp

		err = json.Unmarshal(buf, &jsReq)
		if err != nil {
			h.zap.Warn("invalid JSON format",
				zap.Error(err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		jsResp.Result, err = h.service.AddURL(jsReq.URL)

		if err != nil {
			h.zap.Warn("internal error",
				zap.Error(err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		buf, err = json.Marshal(&jsResp)
		if err != nil {
			h.zap.Warn("internal error",
				zap.Error(err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write(buf); err != nil {
			h.zap.Debug("failed to write response",
				zap.String("event", "network_error"),
				zap.Error(err),
			)
		}
	}
}

// =========== GET /{shortURL} ==================
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

// =========== DEFAULT ==================
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
