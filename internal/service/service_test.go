package service

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"murl/internal/config"
	"murl/internal/mocks"
	"murl/internal/model/event"
)

// Вспомогательный мок конфига
type mockSvcConfig struct {
	baseURL string
}

func (m *mockSvcConfig) ShortBaseURL() config.ShortBaseURL {
	u, _ := url.Parse(m.baseURL)
	return config.ShortBaseURL{URL: *u}
}

func TestService_AddURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepo(ctrl)
	cfg := &mockSvcConfig{baseURL: "http://short.io"}
	svc := NewService(context.Background(), cfg, mockRepo)

	ctx := context.Background()
	original := "https://google.com"
	shortPath := "/.AAQ"

	// Ожидаем вызов Repo.On с событием AddURL
	mockRepo.EXPECT().On(ctx, gomock.Any()).DoAndReturn(
		func(ctx context.Context, e event.Event) (event.Event, error) {
			p, _ := event.GetPayload[event.PayloadAddURL](e)
			assert.Equal(t, original, p.OriginalURL)

			// Возвращаем "обработанное" событие
			p.ShortURL = shortPath
			p.ConflictFlag = false
			return event.MakeEvent(p, e)
		})

	res, err := svc.AddURL(ctx, original)

	require.NoError(t, err)
	assert.Equal(t, "http://short.io/.AAQ", res)
}

func TestService_NormalizeURL_Validation(t *testing.T) {
	svc := &Service{}
	ctx := context.Background()

	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"Valid HTTPS", "https://ya.ru", nil},
		{"Valid HTTP", "http://ya.ru", nil},
		{"No Scheme", "ya.ru", ErrInvalidURLFormat},
		{"FTP Scheme", "ftp://ya.ru", ErrInvalidURLFormat},
		{"Bad Format", "://bad", ErrInvalidURLFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.NormalizeURL(ctx, tt.input)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestService_AddURL_BlockedDomain(t *testing.T) {
	cfg := &mockSvcConfig{baseURL: "http://murl.io"}
	svc := NewService(context.Background(), cfg, nil)

	// Попытка сократить ссылку на самого себя
	_, err := svc.AddURL(context.Background(), "http://murl.io")
	assert.ErrorIs(t, err, ErrDomainIsBlocked)
}

func TestService_GetURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepo(ctrl)
	svc := &Service{repo: mockRepo}

	shortPath := "/.AAQ"
	original := "https://github.com"

	mockRepo.EXPECT().On(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, e event.Event) (event.Event, error) {
			p, _ := event.GetPayload[event.PayloadGetURL](e)
			assert.Equal(t, shortPath, p.ShortURL)

			p.OriginalURL = original
			return event.MakeEvent(p, e)
		})

	res, err := svc.GetURL(context.Background(), shortPath)
	assert.NoError(t, err)
	assert.Equal(t, original, res)
}

func TestService_Batch_PartialError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepo(ctrl)
	cfg := &mockSvcConfig{baseURL: "http://s.io"}
	svc := NewService(context.Background(), cfg, mockRepo)

	batch := event.PayloadBatch{
		Batch: []event.PayloadBatchItem{
			{OriginalURL: "https://ok.com"},
			{OriginalURL: "not-a-url"}, // Ошибка нормализации
		},
	}

	mockRepo.EXPECT().On(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, e event.Event) (event.Event, error) {
			p, _ := event.GetPayload[event.PayloadBatch](e)
			// Проверяем, что первая ссылка нормализована, а вторая получила ошибку
			assert.NotEmpty(t, p.Batch[0].OriginalURL)
			assert.Equal(t, ErrInvalidURLFormat.Error(), p.Batch[1].Err)

			p.Batch[0].ShortURL = "/.OK"
			return event.MakeEvent(p, e)
		})

	res, err := svc.Batch(context.Background(), batch)
	assert.NoError(t, err)
	assert.Equal(t, "http://s.io/.OK", res.Batch[0].ShortURL)
	assert.Equal(t, ErrInvalidURLFormat.Error(), res.Batch[1].Err)
}
