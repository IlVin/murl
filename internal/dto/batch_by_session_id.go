package dto

import "github.com/google/uuid"

// BatchBySessionID представляет собой контейнер для пакетной операции,
// привязанной к конкретной сессии пользователя.
// Используется для массового сокращения ссылок с сохранением авторства.
type BatchBySessionID struct {
	// SessionID — уникальный идентификатор сессии владельца ссылок.
	SessionID uuid.UUID `json:"session_id"`
	// Batch — список элементов (ссылок) для обработки.
	Batch []BatchItem `json:"batch"`
}

// EventType возвращает тип события EvBatchBySessionID.
// Реализует интерфейс для записи пакетной операции в журнал событий.
func (BatchBySessionID) EventType() EvType { return EvBatchBySessionID }
