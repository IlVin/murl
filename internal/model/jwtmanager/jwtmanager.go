package jwtmanager

import (
	"fmt"
	"time"

	"murl/internal/model"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=jwtmanager_mock_test.go -package=$GOPACKAGE

type JWTManagerConfig interface {
	JWTSecretKey() string
	JWTTTL() time.Duration
}

type jwtManager struct {
	secretKey  []byte
	defaultTTL time.Duration
}

type Claims struct {
	jwt.RegisteredClaims
	SessionID uuid.UUID `json:"session_id"`
}

// NewJWT конструктор менеджера токенов
func NewJWT(cfg JWTManagerConfig) (*jwtManager, error) {
	key := cfg.JWTSecretKey()
	if key == "" {
		return nil, fmt.Errorf("jwt secret key is required")
	}

	return &jwtManager{
		secretKey:  []byte(key),
		defaultTTL: cfg.JWTTTL(),
	}, nil
}

// NeedRemaining проверяет, нужно ли продлевать сессию.
// Возвращает true, если до истечения осталось меньше 1/4 от стандартного TTL.
func (m *jwtManager) NeedRemaining(session model.Session) bool {
	if session.TTL.IsZero() {
		return true
	}
	// Если осталось меньше 25% времени жизни — пора обновлять
	return time.Until(session.TTL) < m.defaultTTL/4
}

// GenerateJWT создает новый токен и обновляет TTL в объекте сессии
func (m *jwtManager) GenerateJWT(session model.Session) (string, error) {
	// При генерации всегда продлеваем TTL на стандартную величину
	session.TTL = time.Now().Add(m.defaultTTL)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(session.TTL),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		SessionID: session.ID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secretKey)
}

// VerifyJWT парсит токен и возвращает модель сессии с временем истечения
func (m *jwtManager) VerifyJWT(tokenString string) (model.Session, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secretKey, nil
	})

	if err != nil {
		return model.Session{}, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return model.Session{}, fmt.Errorf("invalid token")
	}

	if claims.SessionID == uuid.Nil {
		return model.Session{}, fmt.Errorf("session_id is missing in claims")
	}

	var expiration time.Time
	if claims.ExpiresAt != nil {
		expiration = claims.ExpiresAt.Time
	}

	return model.Session{
		ID:  claims.SessionID,
		TTL: expiration,
	}, nil
}
