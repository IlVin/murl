package repository

import (
	"context"
	"murl/internal/dto"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=repo_links_by_session_id_mock_test.go -package=$GOPACKAGE

// RepoLinksBySessionID определяет контракт для работы со ссылками, привязанными к конкретной сессии.
// Позволяет не только создавать ссылки, но и управлять ими в контексте конкретного владельца.
type RepoLinksBySessionID interface {
	// UpSert выполняет атомарную операцию вставки или получения существующей ссылки для указанной сессии.
	// Возвращает короткий путь (shortPath), признак конфликта (если URL уже был сокращен ранее) и ошибку.
	UpSert(ctx context.Context, sessionID string, originalURL string) (shortPath string, conflictFlag bool, err error)

	// Set устанавливает принудительное соответствие между оригинальным URL, коротким путем и сессией.
	// Метод полезен при миграции данных или восстановлении состояния из лога событий.
	Set(ctx context.Context, sessionID string, originalURL string, shortPath string) error

	// SelectAll возвращает список всех ссылок (в формате URLItem), принадлежащих указанной сессии.
	// Если ссылок не найдено, должен возвращаться пустой срез и nil в качестве ошибки.
	SelectAll(ctx context.Context, sessionID string) ([]dto.URLItem, error)

	// BatchDelBySessionID выполняет пакетное удаление (или пометку на удаление) списка идентификаторов ссылок.
	// Удаление должно производиться только для тех записей, которые принадлежат переданному sessionID.
	BatchDelBySessionID(ctx context.Context, sessionID string, batch dto.DeleteURLBySessionID) error
}
