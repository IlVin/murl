package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"murl/internal/model/event"
	"murl/internal/service"
	"net/http"
	"strings"

	"github.com/bcicen/jstream"
)

//go:generate mockgen -source=$GOFILE -destination=handlers_mocks_test.go -package=$GOPACKAGE

// Объявляем список используемых параметров конфига
type HandlersConfig interface {
}

// Эти методы сервиса используются хэндлерами
type MicroURLService interface {
	AddURL(ctx context.Context, url string) (string, error)
	Batch(ctx context.Context, e event.Event) (event.Event, error)
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
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("body read failed: %w", err)
	}

	// Если ничего не прислали
	if len(buf) == 0 {
		return nil, errors.New("request body is empty")
	}

	return buf, nil
}

// ErrHandling по ошибке определяет какой http статус выдавать и возвращает ошибку,
// если запрос не может быть успешным
func ErrHandling(err error, statusOK int) (int, error) {
	if err == nil {
		return statusOK, nil
	}

	if errors.Is(err, service.ErrInvalidURLFormat) {
		return http.StatusBadRequest, err
	} else if errors.Is(err, service.ErrDomainIsBlocked) {
		return http.StatusUnprocessableEntity, err
	} else if errors.Is(err, service.ErrQuotaReached) {
		return http.StatusTooManyRequests, err
	} else if errors.Is(err, service.ErrConflict) {
		return http.StatusConflict, nil
	}

	return http.StatusInternalServerError, err
}

// =========== POST / ==================
func (h *Handlers) AddURL() http.HandlerFunc {
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

		longURL := string(buf)
		shortURL, err := h.service.AddURL(r.Context(), longURL)

		httpStatus, err := ErrHandling(err, http.StatusCreated)

		if err != nil {
			slog.Warn("service cannot add URL",
				slog.String("URL", longURL),
				slog.Any("err", err),
			)
			w.WriteHeader(httpStatus)
			return
		}

		slog.Info("URL shortened",
			slog.String("long_url", longURL),
			slog.String("short_url", shortURL),
		)

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(httpStatus)

		if _, err := w.Write([]byte(shortURL)); err != nil {
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

func (h *Handlers) APIShorten() http.HandlerFunc {
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

		longURL := jsReq.URL

		slog.Info("APIShorten",
			slog.String("URL", longURL),
		)
		shortURL, err := h.service.AddURL(r.Context(), longURL)
		httpStatus, err := ErrHandling(err, http.StatusCreated)

		if err != nil {
			slog.Warn("service cannot add URL",
				slog.String("URL", longURL),
				slog.Any("err", err),
			)
			w.WriteHeader(httpStatus)
			return
		}

		slog.Info("URL shortened",
			slog.String("long_url", longURL),
			slog.String("short_url", shortURL),
		)

		jsResp.Result = shortURL

		data, err := json.Marshal(jsResp)
		if err != nil {
			slog.Error("failed to encode response",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)

		if _, err := w.Write(data); err != nil {
			slog.Debug("failed to write response",
				slog.String("event", "network_error"),
				slog.Any("err", err),
			)
		}
	}
}

const partSize int = 1000

func (h *Handlers) writePart(ctx context.Context, part event.PayloadBatch, w http.ResponseWriter, isFirst *bool) error {
	// Создаем событие
	e, err := event.MakeEvent(part, nil)
	if err != nil {
		return fmt.Errorf("internal error: %w", err)
	}

	// Просим сервис обработать событие
	rEv, err := h.service.Batch(ctx, e)
	if err != nil {
		return fmt.Errorf("save to storage failed: %w", err)
	}

	// Вынимаем ответ из события
	p := event.PayloadBatch{}
	if err := rEv.GetPayload(&p); err != nil {
		return fmt.Errorf("event unmarshaling failed: %w", err)
	}

	// Выводим результат
	errs := []error{}
	for i := range p {
		b, err := json.Marshal(p[i])
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if *isFirst {
			if _, err := w.Write([]byte("\n")); err != nil {
				return err
			}
			*isFirst = false
		} else {
			if _, err := w.Write([]byte(",\n")); err != nil {
				return err
			}
		}

		if _, err := w.Write(b); err != nil {
			return err
		}
	}

	return errors.Join(errs...)
}

func (h *Handlers) APIShortenBatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			slog.Warn("invalid Content-Type",
				slog.String("Content-Type", ct),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		isFirst := true

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write([]byte("[")); err != nil {
			slog.Error("internal error",
				slog.Any("err", err),
			)
			return
		}

		slog.Info("APIShortenBatch")

		part := make(event.PayloadBatch, 0, 1000)
		decoder := jstream.NewDecoder(r.Body, 1) // extract JSON values at a depth level of 1
		for mv := range decoder.Stream() {
			v, ok := mv.Value.(map[string]interface{})
			if !ok {
				slog.Error("Bad format of batch item",
					slog.Any("batch_item", mv),
				)
				continue
			}

			// Формируем пайлоад
			p := event.PayloadBatchItem{}
			if cID, ok := v["correlation_id"]; !ok {
				continue
			} else if oURL, ok := v["original_url"]; !ok {
				continue
			} else if p.CorrelationID, ok = cID.(string); !ok {
				continue
			} else if p.OrigURL, ok = oURL.(string); !ok {
				continue
			}

			// Добавляем пайлоад в батч пакет
			part = append(part, p)

			// Если набралось чуток
			if len(part) >= partSize {
				if err := h.writePart(r.Context(), part, w, &isFirst); err != nil {
					slog.Error("internal error",
						slog.Any("err", err),
					)
					return
				}
				// Опустошаем слайс
				clear(part)
				part = part[:0]
			}
		}
		// Если осталось чуток
		if len(part) > 0 {
			if err := h.writePart(r.Context(), part, w, &isFirst); err != nil {
				slog.Error("internal error",
					slog.Any("err", err),
				)
				return
			}
		}

		if _, err := w.Write([]byte("]")); err != nil {
			slog.Error("internal error",
				slog.Any("err", err),
			)
			return
		}
	}
}

// =========== GET /{shortURL} ==================
func (h *Handlers) GetURL() http.HandlerFunc {
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
func (h *Handlers) Ping() http.HandlerFunc {
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
func (h *Handlers) Default() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}
}
