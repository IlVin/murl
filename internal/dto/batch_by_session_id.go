package dto

import "github.com/google/uuid"

//go:generate go run murl/cmd/event $GOFILE

// BatchBySessionID представляет собой контейнер для пакетной операции,
// привязанной к конкретной сессии пользователя.
// Используется для массового сокращения ссылок с сохранением авторства.
//
//generate:event
//generate:reset
type BatchBySessionID struct {
	// SessionID — уникальный идентификатор сессии владельца ссылок.
	SessionID uuid.UUID `json:"session_id"`
	// Batch — список элементов (ссылок) для обработки.
	Batch []BatchItem `json:"batch"`
}
