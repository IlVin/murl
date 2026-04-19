package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"murl/internal/config"
	"murl/internal/model"
	"murl/internal/model/jwtmanager"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=session_mock_test.go -package=$GOPACKAGE

const (
	cookieName string = "murl_session"
)

type TokenManager interface {
	VerifyJWT(token string) (model.Session, error)
	GenerateJWT(session model.Session) (string, error)
	NeedRemaining(session model.Session) bool // возвращает true, если сессию пора обновить
}

type SessionConfig interface {
	KeySession() config.KeySession
	jwtmanager.JWTManagerConfig
}

// setSessionCookie устанавливает JWT в HTTP куку
func setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // В продакшене сменить на true
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
	w.Header().Add("Vary", "Cookie, Authorization")
}

// WithSession проверяет JWT, продлевает его при необходимости и сохраняет сессию в контекст
func WithSession(cfg SessionConfig) func(http.Handler) http.Handler {
	keySession := cfg.KeySession()

	manager, err := jwtmanager.NewJWT(cfg)
	if err != nil {
		// Критическая ошибка конфигурации — не даем запустить сервер
		panic(fmt.Sprintf("jwt manager fail: %v", err))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractToken(r)
			var session model.Session
			var err error
			var sessionExists bool

			// 1. Пытаемся восстановить сессию
			if tokenStr != "" {
				session, err = manager.VerifyJWT(tokenStr)
				if err == nil {
					sessionExists = true

					// --- ЛОГИКА ПРОДЛЕНИЯ ---
					if manager.NeedRemaining(session) {
						slog.Debug("session renewal triggered", slog.String("id", session.ID.String()))

						// Генерируем новый токен (время жизни обновится внутри GenerateJWT или менеджером)
						newToken, err := manager.GenerateJWT(session)
						if err == nil {
							// Перепарсиваем сессию, чтобы получить обновленный TTL для контекста
							if updated, parseErr := manager.VerifyJWT(newToken); parseErr == nil {
								session = updated
							}
							setSessionCookie(w, newToken, session.TTL)
						}
					}
				} else {
					slog.Warn("invalid token, generating guest session", slog.Any("err", err))
				}
			}

			// 2. Создание новой сессии, если старой нет
			if !sessionExists {
				session = model.Session{
					ID: uuid.New(),
				}

				token, err := manager.GenerateJWT(session)
				if err == nil {
					if updated, parseErr := manager.VerifyJWT(token); parseErr == nil {
						session = updated
					}
					setSessionCookie(w, token, session.TTL)
				}
			}

			// 3. Передача в контекст
			ctx := context.WithValue(r.Context(), keySession, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1]
		}
	}
	if cookie, err := r.Cookie(cookieName); err == nil {
		return cookie.Value
	}
	return ""
}
