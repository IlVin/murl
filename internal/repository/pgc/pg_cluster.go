package pgc

import (
	"context"
)

// PgClusterConfig интерфейс конфига для пула шардов
type PgClusterConfig interface {
	ShardSize() byte
	DBDSN() string
}

// PgCluster
type PgCluster interface {

	// ShardID возвращает идентификатор шарда по ключу
	ShardID(key any) byte

	// Close последоавтельно закрывает все шарды пула
	Close() error

	// RunMigrations последовательно запускает миграции на всех шардах пула
	RunMigrations(ctx context.Context) error

	// GetShard возвращает PgInstance указанного шарда
	GetShard(shardID byte) (PgInstance, error)

	// Ping последовательно пингует все шарды пула. Возвращает первую ошибку.
	Ping(ctx context.Context) error

	// Size размер кластера
	Size() byte

	// Записать в БД батчи
	Flush()
}
