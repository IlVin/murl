// Package repository определяет абстракции для сохранения и извлечения данных.
// Пакет содержит интерфейс репозитория, который поддерживает как синхронные операции,
// так и восстановление состояния через систему событий.
package repository

import (
	"context"
	"errors"
	"murl/internal/dto"
	"murl/internal/model/event"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=repo_mock_test.go -package=$GOPACKAGE

// ErrInternalServerError — общая ошибка, возвращаемая при сбоях во внутренней логике хранилищ.
var ErrInternalServerError = errors.New("internal server error")

// RepoConfig определяет интерфейс необходимых настроек для инициализации репозитория.
// Позволяет репозиторию получать параметры драйвера, шардирования и путей к данным.
type RepoConfig interface {
	// RepoDrv возвращает идентификатор драйвера (например, "PgDB" или "InMemory").
	RepoDrv() string
	// ShardSize возвращает количество сегментов данных (шардов).
	ShardSize() byte
	// DBDSN возвращает строку подключения к базе данных.
	DBDSN() string
	// EventStoragePath возвращает путь к файлу лога событий.
	EventStoragePath() string
}

// Repo определяет универсальный интерфейс для всех типов хранилищ приложения.
// Реализации этого интерфейса должны обеспечивать потокобезопасность.
type Repo interface {
	// On выполняет применение события (Event) к текущему состоянию репозитория.
	// Используется для восстановления данных из Event Storage или синхронизации.
	On(context.Context, event.Event) error

	// Ping проверяет доступность физического хранилища (БД или файловой системы).
	Ping(context.Context) error

	// Close корректно завершает работу с репозиторием, закрывая соединения и дескрипторы.
	Close(context.Context) error

	// AddURL регистрирует новую короткую ссылку в анонимном режиме.
	AddURL(context.Context, dto.AddURL) (dto.AddURL, error)

	// AddURLBySessionID регистрирует ссылку с привязкой к конкретному пользователю.
	AddURLBySessionID(context.Context, dto.AddURLBySessionID) (dto.AddURLBySessionID, error)

	// GetURL возвращает информацию об оригинальном URL по его короткому идентификатору.
	GetURL(context.Context, dto.GetURL) (dto.GetURL, error)

	// GetInternalStats возвращает статистику сервиса
	GetInternalStats(context.Context) (dto.Stats, error)

	// GetURLBySessionID возвращает список всех ссылок, созданных владельцем сессии.
	GetURLBySessionID(context.Context, dto.GetURLBySessionID) (dto.GetURLBySessionID, error)

	// DeleteURLBySessionID помечает указанные ссылки как удаленные для владельца сессии.
	DeleteURLBySessionID(context.Context, dto.DeleteURLBySessionID) error

	// Batch выполняет массовую регистрацию ссылок в рамках одной операции.
	Batch(context.Context, dto.Batch) (dto.Batch, error)
}
