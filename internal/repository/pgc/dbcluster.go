package pgc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	config "murl/internal/config"
	"sync/atomic"
)

var ErrShardOutOfPlace = errors.New("Shard is out of place in the config file")
var ErrCannotInitCheckService = errors.New("Cannot init CheckService")

// DBCluster шардированный кластер, который умеет восстанавливать коннекты к БД
type DBCluster struct {
	id string

	ctx context.Context

	rwPool []*PgHndlPool
	roPool []*PgHndlPool

	isOpened atomic.Bool

	checkService *CheckService
}

func NewDBCluster(ctx context.Context, id string, config config.DBClusterProps) (*DBCluster, error) {
	var toCheckService []*PgHndl
	var toRWPool [][]*PgHndl
	var toROPool [][]*PgHndl

	fnClosePgPool := func() {
		for _, h := range toCheckService {
			h.Close()
		}
	}

	for i, shrdCfg := range config.Shards {
		shardID := fmt.Sprintf("%02d", i)
		if shardID != shrdCfg.ShrdID {
			slog.Error(
				"Bad .Shards config",
				slog.String("Position in config", shardID),
				slog.String(".Shards[*].ShrdID", shrdCfg.ShrdID),
			)
			fnClosePgPool()
			return nil, ErrShardOutOfPlace
		}

		rw := make([]*PgHndl, 0, len(shrdCfg.RW))
		for _, connString := range shrdCfg.RW {
			pgHndl, err := NewPgHndl(ctx, shardID, connString)
			if err != nil {
				slog.Error(
					"Bad RW ConnString",
					slog.String(".Shards[*].ShrdID", shrdCfg.ShrdID),
					slog.String("ConnString", connString),
					slog.Any("err", err),
				)
				fnClosePgPool()
				return nil, err
			}
			rw = append(rw, pgHndl)
			toCheckService = append(toCheckService, pgHndl)
		}
		toRWPool = append(toRWPool, rw)

		ro := make([]*PgHndl, 0, len(shrdCfg.RO))
		for _, connString := range shrdCfg.RO {
			pgHndl, err := NewPgHndl(ctx, shardID, connString)
			if err != nil {
				slog.Error(
					"Bad RO ConnString",
					slog.String(".Shards[*].ShrdID", shrdCfg.ShrdID),
					slog.String("ConnString", connString),
					slog.Any("err", err),
				)
				fnClosePgPool()
				return nil, err
			}
			ro = append(ro, pgHndl)
			toCheckService = append(toCheckService, pgHndl)
		}
		toROPool = append(toROPool, ro)
	}

	rwPools := make([]*PgHndlPool, 0, len(toRWPool))
	for _, toP := range toRWPool {
		p, err := NewPgHndlPool(toP)
		if err != nil {
			fnClosePgPool()
			return nil, err
		}
		rwPools = append(rwPools, p)
	}

	roPools := make([]*PgHndlPool, 0, len(toROPool))
	for _, toP := range toROPool {
		p, err := NewPgHndlPool(toP)
		if err != nil {
			fnClosePgPool()
			return nil, err
		}
		roPools = append(roPools, p)
	}

	cService, err := NewCheckService(toCheckService)
	if err != nil {
		fnClosePgPool()
		return nil, errors.Join(ErrCannotInitCheckService, err)
	}

	dbCluster := &DBCluster{
		id:           id,
		ctx:          context.Background(),
		rwPool:       rwPools,
		roPool:       roPools,
		checkService: cService,
	}

	dbCluster.isOpened.Store(false)

	return dbCluster, nil
}

func (c *DBCluster) Open() error {
	if c.isOpened.Load() {
		slog.Error(
			"DBCluster already opened",
			slog.String("clusterID", c.id),
		)
		return fmt.Errorf("BDCluster already open")
	}

	c.checkService.Start(c.ctx)
	c.isOpened.Store(true)

	return nil
}

func (c *DBCluster) RW(shardID int) (*PgHndlPool, error) {
	if shardID < 0 || shardID >= len(c.rwPool) {
		slog.Error(
			"RW Shard not found",
			slog.String("clusterID", c.id),
			slog.Int("shardID", shardID),
		)
		return nil, fmt.Errorf("shardID out of range")
	}
	if !c.isOpened.Load() {
		slog.Error(
			"DBCluster not opened",
			slog.String("clusterID", c.id),
		)
		return nil, fmt.Errorf("BDCluster not opened")
	}
	return c.rwPool[shardID], nil
}

func (c *DBCluster) RO(shardID int) (*PgHndlPool, error) {
	if shardID < 0 || shardID >= len(c.roPool) {
		slog.Error(
			"RO Shard not found",
			slog.String("clusterID", c.id),
			slog.Int("shardID", shardID),
		)
		return nil, fmt.Errorf("shardID out of range")
	}
	if !c.isOpened.Load() {
		slog.Error(
			"DBCluster not opened",
			slog.String("clusterID", c.id),
		)
		return nil, fmt.Errorf("BDCluster not opened")
	}
	return c.roPool[shardID], nil
}

func (c *DBCluster) Close() {
	if !c.isOpened.Load() {
		slog.Error(
			"DBCluster already closed",
			slog.String("clusterID", c.id),
		)
	}

	c.isOpened.Store(false)

	// Останавливаем CheckServide
	c.checkService.Stop()

	// Закрываем соединения к PostgreSQL
	for _, rwPool := range c.rwPool {
		rwPool.Close()
	}
	for _, roPool := range c.roPool {
		roPool.Close()
	}
}
