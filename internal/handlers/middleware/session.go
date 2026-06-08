// Package middleware содержит обработчики промежуточного слоя для HTTP-запросов.
// Модуль обеспечивает сквозную функциональность: логирование, сжатие,
// ограничение нагрузки и управление сессиями.
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=session_mock_test.go -package=$GOPACKAGE

const (
	cookieName string = "murl_session"
)

// TokenManager описывает интерфейс компонента для управления жизненным циклом JWT.
type TokenManager interface {
	VerifyJWT(token string) (model.Session, error)
	GenerateJWT(session model.Session) (string, error)
	NeedRemaining(session model.Session) bool // возвращает true, если сессию пора обновить
}

// SessionConfig объединяет требования к конфигурации для работы Middleware сессий.
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

// WithSession возвращает Middleware, которая идентифицирует пользователя по JWT.
//
// Логика работы:
// 1. Извлекает токен из заголовка Authorization (Bearer) или куки "murl_session".
// 2. Если токен валиден: проверяет необходимость продления через NeedRemaining.
// 3. Если токена нет или он невалиден: автоматически создает новую гостевую сессию.
// 4. Помещает объект model.Session в контекст запроса.
// 5. Устанавливает обновленный или новый токен в HTTP-куку.
func WithSession(cfg SessionConfig) (func(http.Handler) http.Handler, error) {
	keySession := cfg.KeySession()

	manager, err := jwtmanager.NewJWT(cfg)
	if err != nil {
		return nil, fmt.Errorf("jwt manager fail: %w", err)
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
						newToken, errJWT := manager.GenerateJWT(session)
						if errJWT == nil {
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
	}, nil
}

// extractToken выполняет поиск JWT в запросе.
// Приоритет отдается заголовку Authorization, затем проверяются Cookie.
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

// extractTokenGRPC извлекает JWT из gRPC заголовка Authorization (Bearer)
func extractTokenGRPC(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	authHeader := md.Get("authorization")
	if len(authHeader) == 0 || authHeader[0] == "" {
		return ""
	}

	parts := strings.SplitN(authHeader[0], " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}

	return parts[1]
}

// setSessionMetadata отправляет новый или обновленный токен обратно клиенту через gRPC-заголовки ответа
func setSessionMetadata(ctx context.Context, token string) error {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return grpc.SetHeader(ctx, md)
}

// SessionInterceptor идентифицирует пользователя по JWT из Bearer токена.
//
// Логика работы:
// 1. Извлекает токен строго из gRPC заголовка Authorization (Bearer).
// 2. Если токен валиден: проверяет необходимость продления. При продлении отправляет новый JWT обратно в метаданных ответа.
// 3. Если токена нет или он невалиден: автоматически создает новую гостевую сессию и возвращает новый JWT клиенту.
// 4. Помещает объект model.Session в gRPC-контекст запроса.
func SessionInterceptor(cfg SessionConfig) (grpc.UnaryServerInterceptor, error) {
	keySession := cfg.KeySession()

	manager, err := jwtmanager.NewJWT(cfg)
	if err != nil {
		return nil, fmt.Errorf("jwt manager fail: %w", err)
	}

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {

		// 1. Извлекаем JWT из входящего gRPC контекста (только Bearer)
		tokenStr := extractTokenGRPC(ctx)

		var session model.Session
		var sessionExists bool

		// 2. Пытаемся восстановить сессию
		if tokenStr != "" {
			session, err = manager.VerifyJWT(tokenStr)
			if err == nil {
				sessionExists = true

				// --- ЛОГИКА ПРОДЛЕНИЯ ---
				if manager.NeedRemaining(session) {
					slog.Debug("session renewal triggered", slog.String("id", session.ID.String()))

					newToken, errJWT := manager.GenerateJWT(session)
					if errJWT == nil {
						if updated, parseErr := manager.VerifyJWT(newToken); parseErr == nil {
							session = updated
						}
						// Отправляем обновленный токен обратно в заголовках gRPC ответа
						setSessionMetadata(ctx, newToken)
					}
				}
			} else {
				slog.Warn("invalid token, generating guest session", slog.Any("err", err))
			}
		}

		// 3. Создание новой гостевой сессии, если старой нет или она была невалидной
		if !sessionExists {
			session = model.Session{
				ID: uuid.New(),
			}

			token, errJWT := manager.GenerateJWT(session)
			if errJWT == nil {
				if updated, parseErr := manager.VerifyJWT(token); parseErr == nil {
					session = updated
				}
				if err := setSessionMetadata(ctx, token); err != nil {
					slog.Error("session renewal fail",
						slog.Any("err", err),
					)
				}
			}
		}

		// 4. Обогащаем gRPC-контекст сессией и передаем управление дальше в хендлер метода
		newCtx := context.WithValue(ctx, keySession, session)

		return handler(newCtx, req)
	}, nil
}
