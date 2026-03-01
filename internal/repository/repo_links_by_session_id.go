package repository

import (
	"context"
	"murl/internal/model/event"
)

type RepoLinksBySessionID interface {
	// UpSert записывает originalURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
	UpSert(ctx context.Context, sessionID string, originalURL string) (shortPath string, conflictFlag bool, err error)

	// Set установить жесткое соответствие originalURL -> shortPath
	Set(ctx context.Context, sessionID string, originalURL string, shortPath string) error

	// SelectAll получить по sessionID строке все URL в формате [{"short_url": "http://...","original_url": "http://..."},...]
	SelectAll(ctx context.Context, sessionID string) ([]event.PayloadURLItem, error)

	// // BatchUpSert пакетная установка URL
	// BatchUpSert(ctx context.Context, sessionID string, batch event.PayloadBatch) event.PayloadBatch
}
