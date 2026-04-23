package service

import (
	"context"
	"testing"

	"murl/internal/config"
	"murl/internal/dto"
	"murl/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestService_AddURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockRepo(ctrl)
	mockNotifier := NewMockAuditlogNotifier(ctrl)
	mockCfg := NewMockServiceConfig(ctrl)

	u, _ := config.NewShortBaseURL("http://localhost:8080")

	// ВАЖНО: Настраиваем конфиг ДО создания сервиса,
	// так как NewService вызывает методы конфига внутри себя.
	mockCfg.EXPECT().ShortBaseURL().Return(u).AnyTimes()
	mockCfg.EXPECT().KeySession().Return(config.KeySession("session")).AnyTimes()

	svc := NewService(context.Background(), mockCfg, mockRepo, mockNotifier)

	t.Run("Success anonymous", func(t *testing.T) {
		ctx := context.Background()
		original := "https://google.com"
		shortPath := "/.ABC"

		mockRepo.EXPECT().
			AddURL(ctx, gomock.Any()).
			Return(dto.AddURL{ShortURL: shortPath, ConflictFlag: false}, nil)

		mockNotifier.EXPECT().
			Notify(gomock.Any(), gomock.Any()).
			Return(nil)

		res, err := svc.AddURL(ctx, original)
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:8080/.ABC", res)
	})

	t.Run("Recursive shortening blocked", func(t *testing.T) {
		res, err := svc.AddURL(context.Background(), "http://localhost:8080/some-path")
		assert.ErrorIs(t, err, ErrDomainIsBlocked)
		assert.Empty(t, res)
	})
}

func TestService_NormalizeURL(t *testing.T) {
	svc := &Service{}
	ctx := context.Background()

	tests := []struct {
		name    string
		url     string
		wantErr error
	}{
		{"Valid HTTPS", "https://yandex.ru", nil},
		{"Valid HTTP", "http://murl.io", nil},
		{"No scheme", "yandex.ru", ErrInvalidURLFormat},
		{"Unsupported scheme", "ftp://files.com", ErrInvalidURLFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.NormalizeURL(ctx, tt.url)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestService_GetURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockRepo(ctrl)
	mockNotifier := NewMockAuditlogNotifier(ctrl)
	svc := &Service{repo: mockRepo, aNotifier: mockNotifier}
	ctx := context.Background()

	t.Run("Success redirect", func(t *testing.T) {
		short := "/.OK"
		original := "https://target.com"

		mockRepo.EXPECT().
			GetURL(ctx, dto.GetURL{ShortURL: short}).
			Return(dto.GetURL{OriginalURL: original, IsGone: false}, nil)

		mockNotifier.EXPECT().Notify(gomock.Any(), gomock.Any()).Return(nil)

		res, err := svc.GetURL(ctx, short)
		assert.NoError(t, err)
		assert.Equal(t, original, res)
	})

	t.Run("URL is Gone", func(t *testing.T) {
		mockRepo.EXPECT().
			GetURL(ctx, gomock.Any()).
			Return(dto.GetURL{IsGone: true, OriginalURL: "old-url"}, nil)

		res, err := svc.GetURL(ctx, "/.GONE")
		assert.ErrorIs(t, err, ErrGone)
		assert.Equal(t, "old-url", res)
	})
}

func TestService_DeleteURLBySessionID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockRepo(ctrl)
	svc := &Service{repo: mockRepo}

	session := model.Session{ID: uuid.New()}
	urls := []string{"abc"}

	// Вместо MatchedBy используем более простой способ проверки аргументов
	mockRepo.EXPECT().
		DeleteURLBySessionID(gomock.Any(), gomock.Any()).
		Do(func(ctx context.Context, d dto.DeleteURLBySessionID) {
			assert.Equal(t, session.ID, d.SessionID)
			assert.Equal(t, "/abc", d.ShortURLs[0])
		}).
		Return(nil)

	err := svc.DeleteURLBySessionID(context.Background(), session, urls)
	assert.NoError(t, err)
}
