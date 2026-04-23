package dto

// EvType представляет собой перечисляемый тип для идентификации событий в системе.
// Используется для маркировки DTO при их сериализации в Event Storage или при аудите.
type EvType int32

//go:generate $GOPATH/bin/stringer -type=EvType

// Перечисление возможных типов событий.
const (
	// EvUnknown — неизвестный тип события (дефолтное значение).
	EvUnknown EvType = iota
	// EvAddURL — событие добавления новой ссылки анонимным пользователем.
	EvAddURL
	// EvAddURLBySessionID — событие добавления ссылки с привязкой к сессии.
	EvAddURLBySessionID
	// EvGetURL — событие запроса (получения) оригинального URL.
	EvGetURL
	// EvGetURLBySessionID — событие запроса URL конкретным пользователем.
	EvGetURLBySessionID
	// EvDeleteURLBySessionID — событие пометки ссылки на удаление пользователем.
	EvDeleteURLBySessionID
	// EvBatch — событие пакетного добавления ссылок.
	EvBatch
	// EvBatchBySessionID — событие пакетного добавления с привязкой к сессии.
	EvBatchBySessionID
)
