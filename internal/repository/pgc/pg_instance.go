package pgc

import (
	"context"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgxPoolIface интерфейс, который ограничивает методы, передаваемые в коллбек
type PgxPoolIface interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
	Stat() *pgxpool.Stat
}

// PgxTxIface интерфейс, который ограничивает методы, передаваемые в коллбек
type PgxTxIface interface {
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	LargeObjects() pgx.LargeObjects
	Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error)
	Exec(ctx context.Context, sql string, arguments ...any) (commandTag pgconn.CommandTag, err error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PgInstance коннектор, предохраняющий БД от дополнительной нагрузки, когда БД "плохо"
// и предохраняющий приложение от каскадного сбоя при проблемах с БД
type PgInstance interface {
	// RunMigrations запуск миграций на инстансе
	RunMigrations(ctx context.Context) error

	// String стрингер "hostname:port/database"
	String() string

	// Close перевод хэндла в IsClosed && !IsReady режим
	Close() error

	// Ping Проверяет работоспособность БД.
	// По результатам работы устанавливается флаг isReady и сбрасывается счетчик h.failures
	Ping(ctx context.Context) error

	// Tx вызвает коллбек и передает ему открытую транзакцию PostgreSQL
	Tx(ctx context.Context, cb func(ctx context.Context, tx PgxTxIface) error) error

	// PgPool вызывает коллбек и передает ему целяй пул коннектов к PostgreSQL
	PgPool(ctx context.Context, cb func(ctx context.Context, pool PgxPoolIface) error) error
}
