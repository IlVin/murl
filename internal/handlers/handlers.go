package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

// Объявляем список используемых параметров конфига
type HandlersConfig interface {
}

// Эти методы сервиса используются хэндлерами
type MicroURLService interface {
	AddURL(url string) (string, error)
	GetURL(url string) (string, error)
	Ping(ctx context.Context) error
}

type Handlers struct {
	service MicroURLService
}

func NewHandlers(cfg HandlersConfig, service MicroURLService) *Handlers {
	return &Handlers{
		service: service,
	}
}

// =========== POST / ==================
func (h *Handlers) HndlAddURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				slog.Error("cannot close r.Body",
					slog.Any("err", err),
				)
			}
		}()
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("cannot read Body",
				slog.Any("err", err),
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
			slog.Warn("failed to write response",
				slog.String("event", "network_error"),
				slog.Any("err", err),
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
				slog.Error("cannot close r.Body",
					slog.Any("err", err),
				)
			}
		}()

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			slog.Warn("invalid Content-Type",
				slog.String("Content-Type", ct),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		buf, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("cannot read Body",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var jsReq APIShortenReq
		var jsResp TAPIShortenResp

		err = json.Unmarshal(buf, &jsReq)
		if err != nil {
			slog.Warn("invalid JSON format",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		jsResp.Result, err = h.service.AddURL(jsReq.URL)

		if err != nil {
			slog.Warn("internal error",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		buf, err = json.Marshal(&jsResp)
		if err != nil {
			slog.Warn("internal error",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write(buf); err != nil {
			slog.Debug("failed to write response",
				slog.String("event", "network_error"),
				slog.Any("err", err),
			)
		}
	}
}

// =========== GET /{shortURL} ==================
func (h *Handlers) HndlGetURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				slog.Error("cannot close r.Body",
					slog.Any("err", err),
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

// =========== GET /ping ==================
func (h *Handlers) HndlPing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				slog.Error("cannot close r.Body",
					slog.Any("err", err),
				)
			}
		}()

		err := h.service.Ping(r.Context())

		if err != nil {
			slog.Error("cannot ping database",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// =========== DEFAULT ==================
func (h *Handlers) HndlDefault() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				slog.Error("cannot close r.Body",
					slog.Any("err", err),
				)
			}
		}()
		w.WriteHeader(http.StatusBadRequest)
	}
}
