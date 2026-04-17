package adapters

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
	ErrNoContent       = errors.New("order not registered in accrual")
	ErrTooManyRequests = errors.New("rate limited")
	ErrInternalError   = errors.New("accrual internal server error")
)

/*
Стратегия записи в аудит URL:
Мы записываем важные данные, поэтому обрывать HTTP запрос на полуслове и рушить JSON формат нельзя.
Поэтому HTTP клиент полагается только на внутренний таймаут, а не на проброшенный контекст.
Почему мы можем ждать окончания HTTP запроса: потому, что обсервер при завершении работы программы
ждет завершения работы всех своих воркеров, т.е. принцип Gracefull shutdown будет соблюден
*/

type URLAuditlog struct {
	id         string
	httpClient *http.Client
	baseURL    string
}

// NewURLAuditlog конструктор
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

// GetID возвращает ID потребителя нотификаций
func (u *URLAuditlog) GetID() string {
	return u.id
}

// Update отправляет нотификацию в Audit URL
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
	defer resp.Body.Close()
	defer io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))

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
