package dto

import "github.com/google/uuid"

// AuditlogNotification представляет собой структуру данных для внешней системы аудита.
// Содержит информацию о ключевых действиях пользователя, предназначенную для
// долгосрочного хранения и анализа.
type AuditlogNotification struct {
	// UnixTimestamp — время возникновения события в формате Unix Epoch (секунды).
	UnixTimestamp int64 `json:"ts"`
	// Action — тип совершенного действия (например, "shorten" или "follow").
	Action string `json:"action"`
	// UserID — уникальный идентификатор пользователя.
	// Поле может быть nil, если действие совершено анонимно.
	UserID *uuid.UUID `json:"user_id,omitempty"`
	// OrigURL — исходный (длинный) адрес, над которым совершено действие.
	OrigURL string `json:"url"`
}
