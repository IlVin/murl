package dto

import "github.com/google/uuid"

// DeleteURLBySessionID представляет собой объект для группового удаления ссылок.
// Используется для передачи списка идентификаторов (коротких URL), которые
// должны быть помечены как удаленные для конкретной сессии пользователя.
type DeleteURLBySessionID struct {
	// SessionID — уникальный идентификатор пользователя, инициировавшего удаление.
	// Используется для проверки прав владения ссылками.
	SessionID uuid.UUID `json:"session_id,omitempty"`
	// ShortURLs — список коротких идентификаторов ссылок, подлежащих удалению.
	ShortURLs []string `json:"short_urls,omitempty"`
}

// EventType возвращает тип события EvDeleteURLBySessionID.
// Используется для фиксации факта удаления в журнале событий.
func (DeleteURLBySessionID) EventType() EvType { return EvDeleteURLBySessionID }
