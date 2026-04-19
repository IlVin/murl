package repository

import (
	"context"
	"errors"
	"murl/internal/model/event"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=repo_mock_test.go -package=$GOPACKAGE

var ErrInternalServerError = errors.New("internal server error")

// Объявляем список используемых параметров конфига
type RepoConfig interface {
	RepoDrv() string
	ShardSize() byte
	DBDSN() string
	EventStoragePath() string
}

type Repo interface {
	On(ctx context.Context, e event.Event) (event.Event, error)
	Ping(ctx context.Context) error
	Close() error
}
