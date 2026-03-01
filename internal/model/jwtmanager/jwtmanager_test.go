package jwtmanager

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"murl/internal/mocks"
	"murl/internal/model"
)

func TestJWTManager_GenerateAndVerify(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	defaultTTL := time.Hour
	mockCfg := mocks.NewMockJWTManagerConfig(ctrl)
	mockCfg.EXPECT().JWTSecretKey().Return("secret").AnyTimes()
	mockCfg.EXPECT().JWTTTL().Return(defaultTTL).AnyTimes()

	m, _ := NewJWT(mockCfg)
	sessionID := uuid.New()

	// 1. Тестируем генерацию (должна проставить TTL сама)
	s := model.Session{ID: sessionID}
	token, err := m.GenerateJWT(s)
	require.NoError(t, err)

	// 2. Верифицируем и проверяем восстановление TTL
	recovered, err := m.VerifyJWT(token)
	require.NoError(t, err)
	assert.Equal(t, sessionID, recovered.ID)
	// Должно быть примерно Now + 1h
	assert.WithinDuration(t, time.Now().Add(defaultTTL), recovered.TTL, time.Second)
}

func TestJWTManager_NeedRemaining(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	defaultTTL := 100 * time.Minute // Для простоты счета 1/4 = 25 мин
	mockCfg := mocks.NewMockJWTManagerConfig(ctrl)
	mockCfg.EXPECT().JWTSecretKey().Return("secret").AnyTimes()
	mockCfg.EXPECT().JWTTTL().Return(defaultTTL).AnyTimes()

	m, _ := NewJWT(mockCfg)

	tests := []struct {
		name string
		ttl  time.Time
		want bool
	}{
		{
			name: "Plenty of time (80 min left)",
			ttl:  time.Now().Add(80 * time.Minute),
			want: false,
		},
		{
			name: "Time is running out (10 min left)",
			ttl:  time.Now().Add(10 * time.Minute),
			want: true,
		},
		{
			name: "Zero TTL",
			ttl:  time.Time{},
			want: true,
		},
		{
			name: "Exactly threshold (approx)",
			ttl:  time.Now().Add(20 * time.Minute),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := model.Session{TTL: tt.ttl}
			assert.Equal(t, tt.want, m.NeedRemaining(s))
		})
	}
}

func TestJWTManager_Validation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCfg := mocks.NewMockJWTManagerConfig(ctrl)
	mockCfg.EXPECT().JWTSecretKey().Return("key").AnyTimes()
	mockCfg.EXPECT().JWTTTL().Return(time.Minute).AnyTimes()
	m, _ := NewJWT(mockCfg)

	t.Run("Expired", func(t *testing.T) {
		s := model.Session{ID: uuid.New(), TTL: time.Now().Add(-time.Hour)}
		token, _ := m.GenerateJWT(s)
		_, err := m.VerifyJWT(token)
		assert.NoError(t, err)
	})

	t.Run("Empty SessionID", func(t *testing.T) {
		s := model.Session{ID: uuid.Nil}
		token, _ := m.GenerateJWT(s)
		_, err := m.VerifyJWT(token)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session_id is missing")
	})
}
