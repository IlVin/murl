package audit

import (
	"murl/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewURLAuditlog(t *testing.T) {
	id := "test-url-id"
	url := "http://localhost:8080"
	adapter := NewURLAuditlog(id, url)

	assert.Equal(t, id, adapter.id)
	assert.Equal(t, url, adapter.baseURL)
	assert.NotNil(t, adapter.httpClient)
	assert.Equal(t, 10*time.Second, adapter.httpClient.Timeout)
}

func TestURLAuditlog_GetID(t *testing.T) {
	adapter := NewURLAuditlog("worker-1", "")
	assert.Equal(t, "worker-1", adapter.GetID())
}

func TestURLAuditlog_Update(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		notif          domain.Notification
		expectedError  error
		errMsgContains string
	}{
		{
			name: "success 200 ok",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
			},
			notif:         domain.Notification{Message: []byte(`{"status":"ok"}`)},
			expectedError: nil,
		},
		{
			name: "error 204 no content",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			notif:         domain.Notification{Message: []byte(`{}`)},
			expectedError: ErrNoContent,
		},
		{
			name: "error 429 too many requests",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
			},
			notif:         domain.Notification{Message: []byte(`{}`)},
			expectedError: ErrTooManyRequests,
		},
		{
			name: "error 500 internal server error",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			notif:         domain.Notification{Message: []byte(`{}`)},
			expectedError: ErrInternalError,
		},
		{
			name: "unexpected status code 404",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			notif:          domain.Notification{Message: []byte(`{}`)},
			errMsgContains: "unexpected status code: 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Создаем тестовый сервер
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			adapter := NewURLAuditlog("test", server.URL)
			err := adapter.Update(tt.notif)

			if tt.expectedError != nil {
				assert.ErrorIs(t, err, tt.expectedError)
			} else if tt.errMsgContains != "" {
				assert.Contains(t, err.Error(), tt.errMsgContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestURLAuditlog_Update_NetworkError(t *testing.T) {
	// Сценарий: некорректный URL (ошибка NewRequest или Do)
	adapter := NewURLAuditlog("test", "cache_object://invalid-url")
	err := adapter.Update(domain.Notification{Message: []byte("data")})
	assert.Error(t, err)
}

func TestURLAuditlog_Update_Timeout(t *testing.T) {
	// Сценарий: сервер отвечает слишком долго
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond) // Имитируем задержку
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewURLAuditlog("test", server.URL)
	// Искусственно занижаем таймаут для теста
	adapter.httpClient.Timeout = 5 * time.Millisecond

	err := adapter.Update(domain.Notification{Message: []byte("too slow")})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Client.Timeout exceeded")
}
