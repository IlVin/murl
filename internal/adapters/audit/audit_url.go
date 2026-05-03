package audit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"murl/internal/domain"
	"net/http"
	"time"
)

var (
	// ErrNoContent возвращается, если система регистрации вернула такое.
	ErrNoContent = errors.New("no content")
	// ErrTooManyRequests возвращается при превышении лимита запросов (rate limiting).
	ErrTooManyRequests = errors.New("rate limited")
	// ErrInternalError возвращается при внутренней ошибке сервера аудита.
	ErrInternalError = errors.New("accrual internal server error")
)

// URLAuditlog реализует отправку уведомлений во внешнюю систему через HTTP POST.
//
// Стратегия обработки запросов:
// Для обеспечения целостности данных (сохранение корректного JSON-формата)
// HTTP-клиент игнорирует проброшенный контекст и полагается исключительно
// на собственный внутренний Timeout. Это гарантирует, что запрос не будет
// прерван "на полуслове". Безопасность при завершении приложения обеспечивается
// механизмом Graceful Shutdown на уровне обсервера воркеров.
type URLAuditlog struct {
	id         string
	httpClient *http.Client
	baseURL    string
}

// NewURLAuditlog создает новый экземпляр аудитора, отправляющего данные по URL.
// Параметр id используется для идентификации потребителя в системе нотификаций.
func NewURLAuditlog(id string, u string) *URLAuditlog {
	return &URLAuditlog{
		id:      id,
		baseURL: u,
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{MaxIdleConnsPerHost: 100},
		},
	}
}

// GetID возвращает уникальный идентификатор аудитора.
func (u *URLAuditlog) GetID() string {
	return u.id
}

// Update отправляет нотификацию во внешнюю систему.
// Метод преобразует доменную модель уведомления в HTTP POST запрос с JSON-телом.
func (u *URLAuditlog) Update(notif domain.Notification) error {
	req, err := http.NewRequest(http.MethodPost, u.baseURL, bytes.NewReader(notif.Message))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	}()

	switch resp.StatusCode {

	case http.StatusOK, http.StatusCreated, http.StatusAccepted:
		return nil
	case http.StatusNoContent:
		return ErrNoContent
	case http.StatusTooManyRequests:
		return ErrTooManyRequests
	case http.StatusInternalServerError:
		return ErrInternalError
	default:
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
