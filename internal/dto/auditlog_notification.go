package dto

import "github.com/google/uuid"

type AuditlogNotification struct {
	UnixTimestamp int64      `json:"ts"`                // unix timestamp события
	Action        string     `json:"action"`            // действие: shorten (создание) или follow (прохождение по ссылке)
	UserID        *uuid.UUID `json:"user_id,omitempty"` // идентификатор пользователя, если есть
	OrigURL       string     `json:"url"`               // оригинальный (не сокращенный) URL
}
