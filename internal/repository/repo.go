package repository

import (
	"context"
	"errors"
	"murl/internal/model/event"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=mocks/repo_mocks.go -package=mocks
//go:generate $GOPATH/bin/mockgen                 -destination=mocks/pgx_mocks.go  -package=mocks github.com/jackc/pgx/v5 Tx,Row,BatchResults

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
