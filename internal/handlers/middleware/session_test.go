package middleware

import (
	"murl/internal/config"
	"murl/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
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
