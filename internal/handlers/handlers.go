package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const bodyLimit = 1024 * 1024

//go:generate mockgen -source=$GOFILE -destination=handlers_mocks_test.go -package=$GOPACKAGE

// Объявляем список используемых параметров конфига
type HandlersConfig interface {
}

// Эти методы сервиса используются хэндлерами
type MicroURLService interface {
	AddURL(ctx context.Context, url string) (string, error)
	GetURL(ctx context.Context, url string) (string, error)
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

func readBody(r *http.Request) ([]byte, error) {
	// Ограничиваем чтение, чтобы избежать переполнения памяти
	lr := io.LimitReader(r.Body, bodyLimit+1)

	buf, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("body read failed: %w", err)
	}

	if len(buf) > bodyLimit {
		return nil, errors.New("request body too large")
	}

	// Если ничего не прислали
	if len(buf) == 0 {
		return nil, errors.New("request body is empty")
	}

	return buf, nil
}

// =========== POST / ==================
func (h *Handlers) HndlAddURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		buf, err := readBody(r)
		if err != nil {
			slog.Error("request body validation failed",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		murl, err := h.service.AddURL(r.Context(), string(buf))

		if err != nil {
			slog.Warn("service cannot add URL",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
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

type APIShortenResp struct {
	Result string `json:"result"`
}

func (h *Handlers) HndlAPIShorten() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			slog.Warn("invalid Content-Type",
				slog.String("Content-Type", ct),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		buf, err := readBody(r)
		if err != nil {
			slog.Error("request body validation failed",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var jsReq APIShortenReq
		var jsResp APIShortenResp

		err = json.Unmarshal(buf, &jsReq)
		if err != nil {
			slog.Warn("invalid JSON format",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		res, err := h.service.AddURL(r.Context(), jsReq.URL)
		if err != nil {
			slog.Warn("internal error",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		jsResp.Result = res

		data, err := json.Marshal(jsResp)
		if err != nil {
			slog.Error("failed to encode response",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write(data); err != nil {
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
		u, err := h.service.GetURL(r.Context(), r.URL.String())
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Location", u)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}
}

// =========== GET /ping ==================
func (h *Handlers) HndlPing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h.service.Ping(r.Context())

		if err != nil {
			slog.Error("cannot ping database",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// =========== DEFAULT ==================
func (h *Handlers) HndlDefault() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}
}
