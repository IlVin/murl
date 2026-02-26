package pgcluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/model"
	"murl/internal/repository/pgc"
	"murl/internal/repository/pgc/instance"
	"murl/internal/repository/pgc/metrics"
)

// pgCluster
type pgCluster struct {
	instances []*instance.PgInstance
	shards    []*instance.PgInstance
}

// NewPgCluster конструктор пула шардов
func NewPgCluster(ctx context.Context, cfg pgc.PgClusterConfig, metrics *metrics.PrometheusPgMetrics) (pgc.PgCluster, error) {
	shardSize := int(cfg.ShardSize())
	if shardSize <= 0 {
		return nil, errors.New("shard size must be greater than 0")
	}

	c := &pgCluster{
		instances: make([]*instance.PgInstance, 0, 1),
		shards:    make([]*instance.PgInstance, 0, shardSize),
	}

	// В БД на текущий момент шардирования нет,
	// но ожидается горизонтальное масштабирование.
	// Поэтому реализуем абстракцию шардирования, но на одной БД
	pgInstance, err := instance.NewPgInstance(ctx, cfg.DBDSN(), metrics)
	if err != nil {
		c.Close() // Если при инициализации инстанса произошла ошибка, то уже открытые инстансы нужно закрыть
		return nil, fmt.Errorf("failed create PgInstance: %w", err)
	}

	// Один инстанс на все
	c.instances = append(c.instances, pgInstance)

	// Все шарды работают с одним инстансом
	for i := 0; i < shardSize; i++ {
		c.shards = append(c.shards, pgInstance)
	}

	slog.Info("PgCluster initialized",
		slog.Int("shards_count", shardSize),
		slog.Int("unique_instances", len(c.instances)),
	)

	return c, nil
}

// ShardID возвращает идентификатор шарда по ключу
func (c *pgCluster) ShardID(key any) byte {
	return model.ShardID(key, c.Size())
}

// Close последоавтельно закрывает все шарды пула
func (c *pgCluster) Close() error {
	errs := make([]error, 0, len(c.instances))
	for i := range c.instances {
		if c.instances[i] == nil {
			errs = append(errs, errors.New("nil instance"))
			continue
		}
		errs = append(errs, c.instances[i].Close())
	}
	return errors.Join(errs...)
}

// RunMigrations последовательно запускает миграции на всех шардах пула
func (c *pgCluster) RunMigrations(ctx context.Context) error {
	// На каждом инстансе запускаем миграцию
	errs := make([]error, 0, len(c.instances))
	for i := range c.instances {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		slog.Info("Run migration",
			slog.String("instance", c.instances[i].String()),
		)
		errs = append(errs, c.instances[i].RunMigrations(ctx))
	}
	return errors.Join(errs...)
}

// GetShard возвращает PgInstance указанного шарда
func (c *pgCluster) GetShard(shardID byte) (*instance.PgInstance, error) {
	if shardID >= c.Size() {
		return nil, fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, c.Size())
	}
	return c.shards[shardID], nil
}

// Ping последовательно пингует все шарды пула. Возвращает первую ошибку.
func (c *pgCluster) Ping(ctx context.Context) error {
	for i := range c.instances {
		if err := c.instances[i].Ping(ctx); err != nil {
			return err
		}
	}

	return nil
}

// Size размер кластера
func (c *pgCluster) Size() byte {
	return byte(len(c.shards))
}
