package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"murl/internal/config"
	"murl/internal/dto"
	"murl/internal/service"
)

// Вспомогательный мок конфига
type mockSvcConfig struct {
	keySession    config.KeySession
	trustedSubnet *netip.Prefix
}

func (m *mockSvcConfig) KeySession() config.KeySession {
	return m.keySession
}

func (m *mockSvcConfig) TrustedSubnet() *netip.Prefix {
	return m.trustedSubnet
}

func TestHandlers_AddURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockMicroURLService(ctrl)
	cfg := &mockSvcConfig{keySession: "keySession"}
	h := NewHandlers(cfg, mockSvc)

	originalURL := "https://google.com"
	shortURL := "http://short.io"

	mockSvc.EXPECT().
		AddURL(gomock.Any(), originalURL).
		Return(shortURL, nil)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(originalURL))
	w := httptest.NewRecorder()

	h.AddURL()(w, req)

	res := w.Result()
	defer func() { _ = res.Body.Close() }()

	assert.Equal(t, http.StatusCreated, res.StatusCode)
	body, _ := io.ReadAll(res.Body)
	assert.Equal(t, shortURL, string(body))
}

func TestHandlers_AddURL_Conflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockMicroURLService(ctrl)
	cfg := &mockSvcConfig{keySession: "keySession"}
	h := NewHandlers(cfg, mockSvc)

	// Имитируем ошибку конфликта из сервиса
	mockSvc.EXPECT().
		AddURL(gomock.Any(), gomock.Any()).
		Return("http://existing.url", service.ErrConflict)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("https://google.com"))
	w := httptest.NewRecorder()

	h.AddURL()(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandlers_APIShortenBatch_Streaming(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockMicroURLService(ctrl)
	cfg := &mockSvcConfig{keySession: "keySession"}
	h := NewHandlers(cfg, mockSvc)

	// Входной JSON массив
	inputJSON := `[
		{"correlation_id": "1", "original_url": "https://ya.ru"},
		{"correlation_id": "2", "original_url": "https://go.dev"}
	]`

	// Ожидаемое поведение сервиса
	mockSvc.EXPECT().
		Batch(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, p dto.Batch) (dto.Batch, error) {
			// Проставляем "результаты" сокращения
			for i := range p.Batch {
				p.Batch[i].ShortURL = "short_" + p.Batch[i].CorrelationID
			}
			return p, nil
		})

	req := httptest.NewRequest(http.MethodPost, "/api/shorten/batch", strings.NewReader(inputJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.APIShortenBatch()(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	// Проверяем, что на выходе валидный JSON массив
	respBody := w.Body.String()
	assert.True(t, strings.HasPrefix(respBody, "["))
	assert.True(t, strings.HasSuffix(respBody, "]"))
	assert.Contains(t, respBody, `"short_url":"short_1"`)
	assert.Contains(t, respBody, `"short_url":"short_2"`)
}

func TestHandlers_GetURL_Redirect(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockMicroURLService(ctrl)
	cfg := &mockSvcConfig{keySession: "keySession"}
	h := NewHandlers(cfg, mockSvc)

	originalURL := "https://murl.io"
	mockSvc.EXPECT().
		GetURL(gomock.Any(), "/.AAQ").
		Return(originalURL, nil)

	req := httptest.NewRequest(http.MethodGet, "/.AAQ", nil)
	w := httptest.NewRecorder()

	h.GetURL()(w, req)

	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, originalURL, w.Header().Get("Location"))
}

func TestHandlers_Ping_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := NewMockMicroURLService(ctrl)
	cfg := &mockSvcConfig{keySession: "keySession"}
	h := NewHandlers(cfg, mockSvc)

	mockSvc.EXPECT().Ping(gomock.Any()).Return(errors.New("db connection lost"))

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()

	h.Ping()(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandlers_GetInternalStats(t *testing.T) {
	// Парсим тестовую подсеть для конфигурации хэндлера
	subnet, err := netip.ParsePrefix("192.168.1.0/24")
	if err != nil {
		t.Fatalf("failed to parse test subnet: %v", err)
	}

	tests := []struct {
		name           string
		xRealIP        string
		setupMock      func(m *MockMicroURLService)
		expectedStatus int
		expectedBody   string
	}{
		{
			name:    "Success - Trusted IPv4",
			xRealIP: "192.168.1.50", // IP входит в 192.168.1.0/24
			setupMock: func(m *MockMicroURLService) {
				m.EXPECT().
					GetInternalStats(gomock.Any()).
					// Используем реальный тип dto.Stats (поля настройте под вашу структуру)
					Return(dto.Stats{URLs: 100, Users: 10}, nil)
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `{"urls":100,"users":10}`, // Имена полей в JSON должны совпадать с тегами в dto.Stats
		},
		{
			name:           "Forbidden - Untrusted IPv4",
			xRealIP:        "10.0.0.1",
			setupMock:      func(m *MockMicroURLService) {}, // Сервис не вызывается
			expectedStatus: http.StatusForbidden,
			expectedBody:   "",
		},
		{
			name:           "Forbidden - Empty X-Real-IP Header",
			xRealIP:        "",
			setupMock:      func(m *MockMicroURLService) {}, // Сервис не вызывается
			expectedStatus: http.StatusForbidden,
			expectedBody:   "",
		},
		{
			name:    "Error - Service Failure",
			xRealIP: "192.168.1.100",
			setupMock: func(m *MockMicroURLService) {
				m.EXPECT().
					GetInternalStats(gomock.Any()).
					// Возвращаем пустую структуру вместо nil, так как dto.Stats не является указателем
					Return(dto.Stats{}, errors.New("internal database error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockSvc := NewMockMicroURLService(ctrl)
			cfg := &mockSvcConfig{
				keySession:    "keySession",
				trustedSubnet: &subnet,
			}

			h := NewHandlers(cfg, mockSvc)

			tt.setupMock(mockSvc)

			req := httptest.NewRequest(http.MethodGet, "/api/internal/stats", nil)
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			w := httptest.NewRecorder()

			h.GetInternalStats()(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedBody != "" {
				assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
				assert.JSONEq(t, tt.expectedBody, w.Body.String())
			}
		})
	}
}
