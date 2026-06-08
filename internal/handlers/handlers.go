// Package handlers содержит реализацию транспортного слоя (HTTP).
// Пакет отвечает за разбор входящих запросов, валидацию Content-Type,
// вызов соответствующих методов бизнес-логики и формирование HTTP-ответов.
package handlers

// @title           MicroURL Service API
// @version         1.0
// @description     Сервис сокращения ссылок с поддержкой сессий, батчинга и асинхронного удаления.
// @BasePath        /

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
// @description Тип 'Bearer <token>' или использование куки 'murl_session'

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"murl/internal/config"
	"murl/internal/dto"
	"murl/internal/model"
	"murl/internal/service"
	"net/http"
	"net/netip"
	"strings"

	"github.com/bcicen/jstream"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=handlers_mock_test.go -package=$GOPACKAGE

// HandlersConfig определяет набор параметров конфигурации для работы HTTP-хендлеров.
type HandlersConfig interface {
	KeySession() config.KeySession
	TrustedSubnet() *netip.Prefix
}

// MicroURLService описывает интерфейс бизнес-логики, необходимый для работы хендлеров.
// Это позволяет изолировать транспортный слой от конкретной реализации сервиса.
type MicroURLService interface {
	AddURL(ctx context.Context, url string) (string, error)
	Batch(ctx context.Context, e dto.Batch) (dto.Batch, error)
	GetURL(ctx context.Context, url string) (string, error)
	GetURLBySessionID(ctx context.Context, session model.Session) (dto.GetURLBySessionID, error)
	Ping(ctx context.Context) error
	DeleteURLBySessionID(ctx context.Context, session model.Session, data []string) error
	GetInternalStats(ctx context.Context) (dto.Stats, error)
}

// Handlers объединяет все обработчики HTTP-запросов приложения.
type Handlers struct {
	service       MicroURLService
	keySession    config.KeySession
	trustedSubnet *netip.Prefix
}

// NewHandlers — конструктор для создания набора HTTP-обработчиков.
func NewHandlers(cfg HandlersConfig, service MicroURLService) *Handlers {
	return &Handlers{
		service:       service,
		keySession:    cfg.KeySession(),
		trustedSubnet: cfg.TrustedSubnet(),
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

// ErrHandling сопоставляет доменные ошибки сервиса с соответствующими HTTP статус-кодами.
// Возвращает статус и ошибку, если запрос не может быть выполнен.
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
	} else if errors.Is(err, service.ErrNoContent) {
		return http.StatusNoContent, nil
	} else if errors.Is(err, service.ErrGone) {
		return http.StatusGone, nil
	}

	return http.StatusInternalServerError, err
}

// =========== POST / ==================

// AddURL обрабатывает POST запросы с сырым текстом (URL) в теле.
// Возвращает сокращенный URL в текстовом формате.
// @Summary      Сократить URL (Text)
// @Description  Принимает длинный URL в теле запроса как строку и возвращает короткую ссылку.
// @Tags         URL
// @Accept       text/plain
// @Produce      text/plain
// @Param        url  body      string  true  "Оригинальный длинный URL"
// @Success      201  {string}  string  "Короткая ссылка"
// @Success      409  {string}  string  "URL уже сокращен (возвращает существующую ссылку)"
// @Failure      400  {string}  string  "Неверный формат запроса"
// @Failure      422  {string}  string  "Домен заблокирован (циклическое сокращение)"
// @Failure      500  {string}  string  "Внутренняя ошибка сервера"
// @Router       / [post]
func (h *Handlers) AddURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()

		buf, err := readBody(r)
		if err != nil {
			slog.Error("request body validation failed",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		originalURL := string(buf)
		shortURL, err := h.service.AddURL(r.Context(), originalURL)
		httpStatus, err := ErrHandling(err, http.StatusCreated)

		if err != nil {
			slog.Warn("service cannot add URL",
				slog.String("URL", originalURL),
				slog.Any("err", err),
			)
			w.WriteHeader(httpStatus)
			return
		}

		slog.Info("URL shortened",
			slog.String("original_url", originalURL),
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

// =========== GET /api/user/urls ==================

// APIUserURLs возвращает список всех ссылок, принадлежащих текущему авторизованному пользователю.
// @Summary      Ссылки пользователя
// @Description  Возвращает список всех когда-либо сокращенных пользователем ссылок. Требуется авторизация (Cookie или Bearer).
// @Tags         User
// @Produce      json
// @Security     ApiKeyAuth
// @Success      200  {array}   dto.URLItem  "Список ссылок"
// @Success      204  {string}  string       "У пользователя нет ссылок"
// @Failure      401  {string}  string       "Не авторизован"
// @Router       /api/user/urls [get]
func (h *Handlers) APIUserURLs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := model.GetSession(r.Context(), h.keySession)
		if !ok {
			slog.Warn("APIUserURLs unauthorized request")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		slog.Info("APIUserURLs",
			slog.Bool("hasSession", ok),
		)

		URLs, err := h.service.GetURLBySessionID(r.Context(), session)
		httpStatus, err := ErrHandling(err, http.StatusOK)

		if err != nil {
			slog.Warn("service cannot GetURLBySessionID",
				slog.String("sessionID", session.ID.String()),
				slog.Any("err", err),
			)
			w.WriteHeader(httpStatus)
			return
		}

		data, err := json.Marshal(URLs.Result)
		if err != nil {
			slog.Error("failed to encode response",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		slog.Info("APIUserURLs",
			slog.String("JSON", string(data)),
		)
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

// =========== DELETE /api/user/urls ==================

// DeleteAPIUserURLs принимает массив идентификаторов ссылок в JSON для их последующего удаления.
// @Summary      Удалить ссылки (Batch)
// @Description  Принимает список ID ссылок и асинхронно помечает их как удаленные. Только для владельца.
// @Tags         User
// @Accept       json
// @Security     ApiKeyAuth
// @Param        ids  body  []string  true  "Массив ID коротких ссылок"
// @Success      202  {string}  string    "Принято к обработке"
// @Failure      401  {string}  string    "Не авторизован"
// @Router       /api/user/urls [delete]
func (h *Handlers) DeleteAPIUserURLs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := model.GetSession(r.Context(), h.keySession)
		if !ok {
			slog.Warn("APIUserURLs unauthorized request")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		slog.Info("APIUserURLs",
			slog.Bool("hasSession", ok),
			slog.Any("session", session),
		)

		defer func() { _ = r.Body.Close() }()

		buf, err := readBody(r)
		if err != nil {
			slog.Error("request body validation failed",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		data := make([]string, 0, 100)
		err = json.Unmarshal(buf, &data)
		if err != nil {
			slog.Error("request body validation failed",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := h.service.DeleteURLBySessionID(r.Context(), session, data); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusAccepted)
	}
}

// =========== POST /api/shorten ==================

// APIShortenReq описывает структуру входящего JSON-запроса для сокращения ссылки.
type APIShortenReq struct {
	URL string `json:"url"`
}

// APIShortenResp описывает структуру исходящего JSON-ответа с сокращенной ссылкой.
type APIShortenResp struct {
	Result string `json:"result"`
}

// APIShorten обрабатывает POST запросы с JSON объектом {"url": "..."}.
// Возвращает JSON объект {"result": "..."}.
// @Summary      Сократить URL (JSON)
// @Description  Принимает JSON {"url": "..."} и возвращает JSON {"result": "..."}.
// @Tags         URL API
// @Accept       json
// @Produce      json
// @Param        request body      APIShortenReq  true  "Запрос сокращения"
// @Success      201     {object}  APIShortenResp "Успешное создание"
// @Success      409     {object}  APIShortenResp "Конфликт: уже существует"
// @Failure      400     {string}  string         "Неверный формат JSON или URL"
// @Failure      500     {string}  string         "Внутренняя ошибка"
// @Router       /api/shorten [post]
func (h *Handlers) APIShorten() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()

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

func (h *Handlers) writePart(ctx context.Context, part dto.Batch, w http.ResponseWriter, isFirst *bool) error {
	// Просим сервис обработать batch
	p, err := h.service.Batch(ctx, part)
	if err != nil {
		return fmt.Errorf("save to storage failed: %w", err)
	}

	// Выводим результат
	errs := []error{}
	for i := range p.Batch {
		b, err := json.Marshal(p.Batch[i])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		slog.Info("batch item",
			slog.String("b", string(b)),
		)

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

// APIShortenBatch реализует потоковую (streaming) обработку больших массивов ссылок.
// Использует jstream для чтения элементов один за другим, не загружая весь JSON в память.
// Данные обрабатываются пачками (chunks) и сразу записываются в ResponseWriter в формате JSON-массива.
// @Summary      Пакетное сокращение (Streaming)
// @Description  Принимает массив объектов и возвращает массив сокращенных ссылок. Поддерживает большие объемы данных.
// @Tags         URL API
// @Accept       json
// @Produce      json
// @Param        request body      []dto.BatchItem  true  "Массив ссылок для сокращения"
// @Success      201     {array}   dto.BatchItem   "Массив результатов"
// @Failure      400     {string}  string           "Неверный формат"
// @Router       /api/shorten/batch [post]
func (h *Handlers) APIShortenBatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()

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

		part := make([]dto.BatchItem, 0, 1000)
		decoder := jstream.NewDecoder(r.Body, 1) // extract JSON values at a depth level of 1
		for mv := range decoder.Stream() {
			v, ok := mv.Value.(map[string]any)
			if !ok {
				slog.Error("Bad format of batch item",
					slog.Any("batch_item", mv),
				)
				continue
			}

			// Формируем пайлоад
			p := dto.BatchItem{}
			if cID, ok := v["correlation_id"]; !ok {
				continue
			} else if oURL, ok := v["original_url"]; !ok {
				continue
			} else if p.CorrelationID, ok = cID.(string); !ok {
				continue
			} else if p.OriginalURL, ok = oURL.(string); !ok {
				continue
			}

			// Добавляем пайлоад в батч пакет
			part = append(part, p)

			// Если набралось чуток
			if len(part) >= partSize {
				if err := h.writePart(r.Context(), dto.Batch{Batch: part}, w, &isFirst); err != nil {
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
			if err := h.writePart(r.Context(), dto.Batch{Batch: part}, w, &isFirst); err != nil {
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

// GetURL обрабатывает GET запросы и выполняет редирект (307 Temporary Redirect) на оригинальный адрес.
// Возвращает 410 Gone, если ссылка была удалена.
// @Summary      Переход по ссылке
// @Description  Возвращает 307 статус и заголовок Location для перехода.
// @Tags         Redirect
// @Param        id  path  string  true  "ID короткой ссылки"
// @Success      307  {string}  string  "Redirect"
// @Failure      410  {string}  string  "Ссылка удалена"
// @Failure      404  {string}  string  "Не найдено"
// @Router       /{id} [get]
func (h *Handlers) GetURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := h.service.GetURL(r.Context(), r.URL.String())
		httpStatus, err := ErrHandling(err, http.StatusTemporaryRedirect)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Location", u)
		w.WriteHeader(httpStatus)
	}
}

// =========== GET /ping ==================

// Ping проверяет доступность базы данных.
// @Summary      Health Check
// @Description  Проверка доступности соединения с хранилищем.
// @Tags         System
// @Success      200  {string}  string  "OK"
// @Failure      500  {string}  string  "DB unreachable"
// @Router       /ping [get]
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

// =========== GET /api/internal/stats ==================

// GetInternalStats статистика сервиса
// Возвращает статистику сервиса для доверенной сети
// @Summary      Статистика сервиса
// @Description  Проверяет доступ и возвращает JSON со статистикой
// @Success      200  {string}  string  "JSON"
// @Failure      403  {string}  string  "Доступ запрещен"
// @Failure      500  {string}  string  "Внутренняя ошибка сервера"
// @Router       /api/internal/stats
func (h *Handlers) GetInternalStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.Header.Get("X-Real-IP")

		isTrusted, err := isIPInPrefix(h.trustedSubnet, clientIP)
		if !isTrusted {
			if err != nil {
				slog.Error("access denied",
					slog.Any("trusted_subnet", h.trustedSubnet),
					slog.String("x_real_ip", clientIP),
					slog.Any("err", err),
				)
			}
			w.WriteHeader(http.StatusForbidden)
			return
		}

		jsResp, err := h.service.GetInternalStats(r.Context())
		if err != nil {
			slog.Error("failed to get internal stats",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		data, err := json.Marshal(jsResp)
		if err != nil {
			slog.Error("failed to encode response",
				slog.Any("err", err),
			)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write(data); err != nil {
			slog.Debug("failed to write response",
				slog.Any("err", err),
			)
		}
	}
}

// isIPInPrefix проверяет, входит ли IP из заголовка в указанную подсеть
func isIPInPrefix(prefix *netip.Prefix, ipStr string) (bool, error) {
	if prefix == nil {
		return false, errors.New("trusted subnet not specified")
	}

	if ipStr == "" {
		return false, errors.New("X-Real-IP header is empty")
	}

	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return false, fmt.Errorf("invalid IP format: %w", err)
	}

	return prefix.Contains(ip), nil
}

// =========== DEFAULT ==================

// Default — хендлер-заглушка для обработки неопределенных маршрутов.
func (h *Handlers) Default() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}
}
