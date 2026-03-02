package repository

import (
	"context"
	"murl/internal/model/event"
)

type RepoLinks interface {
	// UpSert записывает originalURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
	UpSert(ctx context.Context, originalURL string) (shortPath string, conflictFlag bool, err error)

	// Select получить по shortPath строке originalURL строку
	Select(ctx context.Context, shortPath string) (originalURL string, deleted bool, err error)

	// Set установить жесткое соответствие originalURL -> shortPath
	Set(ctx context.Context, originalURL string, shortPath string) error

	// BatchUpSert пакетная установка URL
	BatchUpSert(ctx context.Context, batch event.PayloadBatch) event.PayloadBatch
}
