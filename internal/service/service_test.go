package service

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"murl/internal/config"
	"murl/internal/mocks" // Предполагаем, что моки Repo и Notifier лежат здесь
	"murl/internal/model/event"
)

type mockSvcConfig struct {
	keySession config.KeySession
	baseURL    string
}

func (m *mockSvcConfig) ShortBaseURL() config.ShortBaseURL {
	u, _ := url.Parse(m.baseURL)
	return config.ShortBaseURL{URL: *u}
}

func (m *mockSvcConfig) KeySession() config.KeySession {
	return m.keySession
}

func TestService_AddURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepo(ctrl)
	mockNotifier := mocks.NewMockAuditlogNotifier(ctrl) // Нужен мок нотификатора

	cfg := &mockSvcConfig{baseURL: "http://short.io"}
	// В конструкторе теперь 4 параметра
	svc := NewService(context.Background(), cfg, mockRepo, mockNotifier)

	ctx := context.Background()
	original := "https://google.com"

	// 1. Ожидаем запись в репозиторий
	mockRepo.EXPECT().On(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, e event.Event) (event.Event, error) {
			p, _ := event.GetPayload[event.PayloadAddURL](e)
			p.ShortURL = "/.AAQ"
			return event.MakeEvent(p, e)
		})

	// 2. Ожидаем отправку нотификации (обязательно, иначе будет паника)
	mockNotifier.EXPECT().Notify(gomock.Any(), gomock.Any()).Return(nil)

	res, err := svc.AddURL(ctx, original)

	require.NoError(t, err)
	assert.Equal(t, "http://short.io/.AAQ", res)
}

func TestService_GetURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepo(ctrl)
	mockNotifier := mocks.NewMockAuditlogNotifier(ctrl)

	cfg := &mockSvcConfig{baseURL: "http://short.io"}
	svc := NewService(context.Background(), cfg, mockRepo, mockNotifier)

	shortPath := "/.AAQ"
	original := "https://github.com"

	mockRepo.EXPECT().On(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, e event.Event) (event.Event, error) {
			p := event.PayloadGetURL{OriginalURL: original}
			return event.MakeEvent(p, e)
		})

	// Ожидаем нотификацию типа "follow"
	mockNotifier.EXPECT().Notify(gomock.Any(), gomock.Any()).Return(nil)

	res, err := svc.GetURL(context.Background(), shortPath)
	assert.NoError(t, err)
	assert.Equal(t, original, res)
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
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockNotifier := mocks.NewMockAuditlogNotifier(ctrl)
	cfg := &mockSvcConfig{baseURL: "http://murl.io"}
	svc := NewService(context.Background(), cfg, nil, mockNotifier)

	_, err := svc.AddURL(context.Background(), "http://murl.io")
	assert.ErrorIs(t, err, ErrDomainIsBlocked)
}

func TestService_Batch_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mocks.NewMockRepo(ctrl)
	// Добавляем слеш в конце baseURL для чистоты,
	// хотя url.Parse и JoinPath обычно справляются сами
	cfg := &mockSvcConfig{baseURL: "http://s.io"}
	svc := NewService(context.Background(), cfg, mockRepo, nil)

	batch := event.PayloadBatch{
		Batch: []event.PayloadBatchItem{
			{OriginalURL: "https://ok.com"},
		},
	}

	mockRepo.EXPECT().On(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, e event.Event) (event.Event, error) {
			p, _ := event.GetPayload[event.PayloadBatch](e)
			// Устанавливаем ShortURL для первого элемента пакета
			p.Batch[0].ShortURL = "short"
			return event.MakeEvent(p, e)
		})

	res, err := svc.Batch(context.Background(), batch)

	require.NoError(t, err)
	// Исправлено: ожидаем полный URL, который генерирует сервис
	assert.Equal(t, "http://s.io/short", res.Batch[0].ShortURL)
}
