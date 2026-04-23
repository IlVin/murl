package dto

import "github.com/google/uuid"

// AddURLBySessionID представляет собой расширенную структуру для добавления URL,
// привязанную к конкретной сессии пользователя.
// Используется в сценариях, где необходимо отслеживать авторство ссылки.
type AddURLBySessionID struct {
	// AddURL содержит базовую информацию о ссылке (Original URL).
	AddURL
	// SessionID — уникальный идентификатор сессии пользователя.
	SessionID uuid.UUID `json:"session_id"`
}

// EventType возвращает тип события EvAddURLBySessionID.
// Реализует интерфейс события для системы логов или очередей.
func (AddURLBySessionID) EventType() EvType { return EvAddURLBySessionID }
