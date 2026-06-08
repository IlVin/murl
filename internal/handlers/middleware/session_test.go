package middleware

import (
	"context"
	"murl/internal/config"
	"murl/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestWithSession_Integration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	// 1. Мокаем конфиг, который запрашивает NewJWT внутри миддлвари
	mockCfg := NewMockSessionConfig(ctrl)
	// Настраиваем обязательные параметры для инициализации jwtmanager
	secret := "secret-key-32-chars-length-needed"
	ttl := time.Hour
	keySession := config.KeySession("keySession")
	mockCfg.EXPECT().JWTSecretKey().Return(secret).AnyTimes()
	mockCfg.EXPECT().JWTTTL().Return(ttl).AnyTimes()
	mockCfg.EXPECT().KeySession().Return(keySession).AnyTimes()
	// 2. Хендлер-заглушка для проверки контекста
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := model.GetSession(r.Context(), keySession)
		if ok && s.ID.String() != "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	// Инициализируем миддлварь (теперь передаем конфиг)
	mwSession, err := WithSession(mockCfg)
	assert.NoError(t, err)
	mw := mwSession(nextHandler)

	t.Run("New guest session (No Cookie)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		// Проверяем, что кука установилась
		setCookie := w.Header().Get("Set-Cookie")
		assert.Contains(t, setCookie, cookieName)
		assert.Contains(t, w.Header().Get("Vary"), "Cookie")
	})
	t.Run("Valid existing session (With Cookie)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		// Специально кривой токен
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "invalid-token"})
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		// Должна прилететь новая кука взамен битой
		assert.Contains(t, w.Header().Get("Set-Cookie"), cookieName)
	})
}

func TestSessionInterceptor_Integration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 1. Мокаем конфиг аналогично HTTP-тесту
	mockCfg := NewMockSessionConfig(ctrl)
	secret := "secret-key-32-chars-length-needed"
	ttl := time.Hour
	keySession := config.KeySession("keySession")

	mockCfg.EXPECT().JWTSecretKey().Return(secret).AnyTimes()
	mockCfg.EXPECT().JWTTTL().Return(ttl).AnyTimes()
	mockCfg.EXPECT().KeySession().Return(keySession).AnyTimes()

	// Инициализируем gRPC интерцептор
	interceptor, err := SessionInterceptor(mockCfg)
	assert.NoError(t, err)

	// Заглушка для gRPC информации о методе
	info := &grpc.UnaryServerInfo{
		FullMethod: "/proto.ShortenerService/ShortenURL",
	}

	t.Run("New guest session (No Bearer Token)", func(t *testing.T) {
		// Хендлер-заглушка проверяет, что сессия успешно попала в gRPC-контекст
		handler := func(ctx context.Context, req any) (any, error) {
			s, ok := model.GetSession(ctx, keySession)
			assert.True(t, ok)
			assert.NotEmpty(t, s.ID.String())
			return "response", nil
		}

		// Создаем пустой входящий контекст
		ctx := context.Background()

		// Вызываем интерцептор
		resp, err := interceptor(ctx, "request", info, handler)
		assert.NoError(t, err)
		assert.Equal(t, "response", resp)
	})

	t.Run("Invalid existing session (With Bad Bearer Token)", func(t *testing.T) {
		// Хендлер ожидает, что несмотря на битый токен, интерцептор сгенерирует новую сессию
		handler := func(ctx context.Context, req any) (any, error) {
			s, ok := model.GetSession(ctx, keySession)
			assert.True(t, ok)
			assert.NotEmpty(t, s.ID.String())
			return "response", nil
		}

		// Передаем некорректный токен в gRPC метаданных (аналог плохой куки)
		md := metadata.Pairs("authorization", "Bearer invalid-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)

		resp, err := interceptor(ctx, "request", info, handler)
		assert.NoError(t, err)
		assert.Equal(t, "response", resp)
	})
}
