package repository

import (
	"context"
	"murl/internal/dto"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=repo_links_mock_test.go -package=$GOPACKAGE

// RepoLinks определяет контракт для работы с хранилищем связей между оригинальными и короткими URL.
type RepoLinks interface {
	// UpSert выполняет атомарную операцию вставки или получения существующей ссылки.
	// Возвращает короткий путь (shortPath), признак того, что ссылка уже существовала (conflictFlag), и ошибку.
	UpSert(ctx context.Context, originalURL string) (shortPath string, conflictFlag bool, err error)

	// Select возвращает оригинальный URL по его короткому пути.
	// Возвращает originalURL, признак того, что ссылка помечена как удаленная (deleted), и ошибку.
	Select(ctx context.Context, shortPath string) (originalURL string, deleted bool, err error)

	// Set принудительно устанавливает соответствие между оригинальным URL и коротким путем.
	// Используется при восстановлении состояния или миграциях.
	Set(ctx context.Context, originalURL string, shortPath string) error

	// BatchUpSert выполняет пакетную обработку нескольких URL за один запрос к хранилищу.
	// Возвращает обновленный объект Batch с заполненными короткими ссылками и флагами конфликтов.
	BatchUpSert(ctx context.Context, batch dto.Batch) dto.Batch
}
