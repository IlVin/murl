// Package jwtmanager реализует механизмы создания и валидации JSON Web Tokens (JWT).
// Обеспечивает аутентификацию пользователей через SessionID и управление временем жизни сессий.
package jwtmanager

import (
	"fmt"
	"time"

	"murl/internal/model"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=jwtmanager_mock_test.go -package=$GOPACKAGE

// JWTManagerConfig определяет интерфейс необходимых настроек для работы с токенами.
// Реализуется структурой конфигурации приложения.
type JWTManagerConfig interface {
	JWTSecretKey() string
	JWTTTL() time.Duration
}

// jwtManager инкапсулирует логику работы с JWT, используя алгоритм HS256.
type jwtManager struct {
	secretKey  []byte
	defaultTTL time.Duration
}

// Claims представляет полезную нагрузку (payload) токена.
// Содержит стандартные поля JWT и уникальный идентификатор сессии.
type Claims struct {
	jwt.RegisteredClaims
	SessionID uuid.UUID `json:"session_id"`
}

// NewJWT — конструктор менеджера токенов.
// Возвращает ошибку, если в конфигурации отсутствует секретный ключ.
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

// NeedRemaining проверяет необходимость обновления (продления) токена.
// Возвращает true, если срок действия сессии истек или до его окончания осталось
// менее 25% от установленного в системе времени жизни (TTL).
func (m *jwtManager) NeedRemaining(session model.Session) bool {
	if session.TTL.IsZero() {
		return true
	}
	// Если осталось меньше 25% времени жизни — пора обновлять
	return time.Until(session.TTL) < m.defaultTTL/4
}

// GenerateJWT создает подписанную строковую версию JWT токена на основе данных сессии.
// При вызове метод автоматически продлевает время жизни сессии (session.TTL)
// на значение defaultTTL от текущего момента.
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

// VerifyJWT проверяет подлинность токена и извлекает из него данные сессии.
// Выполняет проверку алгоритма подписи и срока действия токена.
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
