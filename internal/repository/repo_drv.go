package repository

import (
	"context"
	"fmt"
	"murl/internal/model/event"
)

const defaultCap int = 300

const errInternalServerError string = "internal server error"

//go:generate mockgen -source=$GOFILE -destination=repo_drv_mocks_test.go -package=$GOPACKAGE
//go:generate mockgen -destination=pgx_mocks_test.go -package=$GOPACKAGE github.com/jackc/pgx/v5 Tx,Row

type RepoDrvConfig interface {
	RepoDrv() string
	ShardSize() byte
	DBDSN() string
	EventStoragePath() string
}

type RepoDrv interface {
	UpSert(ctx context.Context, shardID byte, str string) (uint64, error)
	BatchUpSert(ctx context.Context, batch []event.PayloadBatchItem) ([]event.PayloadBatchItem, error)
	Select(ctx context.Context, shardID byte, idx uint64) (string, error)
	Set(ctx context.Context, shardID byte, idx uint64, u string) error
	Ping(ctx context.Context) error
}

func NewRepoDrv(ctx context.Context, cfg RepoDrvConfig) (RepoDrv, error) {
	switch cfg.RepoDrv() {
	case "InMemory":
		return newInMemoryRepoDrv(cfg), nil
	case "PgDB":
		drv, err := newPgRepoDrv(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to init driver PgDB: %w", err)
		}
		return drv, nil
	}

	return nil, fmt.Errorf("unknown repo driver: %s", cfg.RepoDrv())
}
