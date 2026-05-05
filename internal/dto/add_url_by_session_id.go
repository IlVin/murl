package dto

import "github.com/google/uuid"

//go:generate go run murl/cmd/event $GOFILE

// AddURLBySessionID представляет собой расширенную структуру для добавления URL,
// привязанную к конкретной сессии пользователя.
// Используется в сценариях, где необходимо отслеживать авторство ссылки.
//
//go:event
type AddURLBySessionID struct {
	// AddURL содержит базовую информацию о ссылке (Original URL).
	AddURL
	// SessionID — уникальный идентификатор сессии пользователя.
	SessionID uuid.UUID `json:"session_id"`
}
