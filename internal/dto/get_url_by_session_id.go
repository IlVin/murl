package dto

import "github.com/google/uuid"

// URLItem представляет собой описание сокращенной ссылки в списке результатов.
// Используется для отображения истории пользователя или результатов поиска.
type URLItem struct {
	// CorrelationID — идентификатор, связывающий запись с исходным запросом (если применимо).
	CorrelationID string `json:"correlation_id,omitempty"`
	// OriginalURL — исходный длинный URL.
	OriginalURL string `json:"original_url,omitempty"`
	// ShortURL — соответствующий короткий URL.
	ShortURL string `json:"short_url,omitempty"`
}

//go:generate go run murl/cmd/event $GOFILE

// GetURLBySessionID представляет собой объект передачи данных, содержащий
// все ссылки, принадлежащие конкретной сессии пользователя.
//
//go:event
type GetURLBySessionID struct {
	// SessionID — уникальный идентификатор владельца ссылок.
	SessionID uuid.UUID `json:"session_id,omitempty"`
	// Result — список найденных ссылок.
	Result []URLItem `json:"result,omitempty"`
}
