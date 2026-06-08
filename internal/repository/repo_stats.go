package repository

import (
	"context"
	"murl/internal/dto"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=repo_stats_mock_test.go -package=$GOPACKAGE

// RepoLinks определяет контракт для работы с хранилищем связей между оригинальными и короткими URL.
type RepoStats interface {
	// GetStats возвращает статистику
	GetStats(ctx context.Context) (dto.Stats, error)
}
