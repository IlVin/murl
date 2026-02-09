package repository

import (
	"context"
	"fmt"
	"log/slog"
	pgc "murl/internal/repository/pgc"
	"sync"

	"golang.org/x/sys/cpu"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RepoDrvConfig interface {
	RepoDrv() string
	ShardSize() byte
	DBDSN() string
	EventStoragePath() string
}

type RepoDrv interface {
	UpSert(ctx context.Context, shardID byte, str string) (uint64, error)
	Select(ctx context.Context, shardID byte, idx uint64) (string, error)
	Set(ctx context.Context, shardID byte, idx uint64, u string) error
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

	return nil, fmt.Errorf("Unknown repo driver: %s", cfg.RepoDrv())
}

// =============================================
//
//	InMemory
//
// =============================================
type inMemoryShard struct {
	mu      sync.RWMutex
	data    map[uint64]string
	index   map[string]uint64 // Для O(1) поиска при UpSert
	lastIdx uint64
	_       cpu.CacheLinePad
}

type InMemoryRepoDrv struct {
	shards []inMemoryShard
}

func newInMemoryRepoDrv(cfg RepoDrvConfig) RepoDrv {
	shardSize := int(cfg.ShardSize())

	s := &InMemoryRepoDrv{
		shards: make([]inMemoryShard, shardSize),
	}

	for i := 0; i < shardSize; i++ {
		s.shards[i] = inMemoryShard{
			data:  make(map[uint64]string, 1000),
			index: make(map[string]uint64, 1000),
		}
	}

	slog.Info("Use InMemoryDrv")
	return s
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
func (s *InMemoryRepoDrv) UpSert(ctx context.Context, shardID byte, str string) (uint64, error) {
	if int(shardID) >= len(s.shards) {
		return 0, fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	shard := &s.shards[shardID]

	shard.mu.RLock()
	if idx, ok := shard.index[str]; ok {
		shard.mu.RUnlock()
		return idx, nil
	}
	shard.mu.RUnlock()

	shard.mu.Lock()
	if idx, ok := shard.index[str]; ok {
		shard.mu.Unlock()
		return idx, nil
	}
	idx := shard.lastIdx
	shard.lastIdx++
	shard.data[idx] = str
	shard.index[str] = idx
	shard.mu.Unlock()

	return idx, nil
}

func (s *InMemoryRepoDrv) Set(ctx context.Context, shardID byte, idx uint64, u string) error {
	if int(shardID) >= len(s.shards) {
		return fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	shard := &s.shards[shardID]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Синхронно обновляем и данные и индекс
	if val, ok := shard.data[idx]; ok {
		delete(shard.index, val)
	}

	shard.data[idx] = u
	shard.index[u] = idx
	if idx >= shard.lastIdx {
		shard.lastIdx = idx + 1
	}

	return nil
}

// По строке-идентификатору возвращает ранее записанную строку
func (s *InMemoryRepoDrv) Select(ctx context.Context, shardID byte, idx uint64) (string, error) {
	if int(shardID) >= len(s.shards) {
		return "", fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	shard := &s.shards[shardID]
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	if val, ok := shard.data[idx]; ok {
		return val, nil
	}

	return "", fmt.Errorf("record not found: %d", idx)
}

// =============================================
//
//	PgDrv
//
// =============================================

//go:generate mockgen -source=$GOFILE -destination=repo_drv_mocks_test.go -package=$GOPACKAGE
//go:generate mockgen -destination=pgx_mocks_test.go -package=$GOPACKAGE github.com/jackc/pgx/v5 Tx,Row

// Чтобы протестировать драйвер постгреса, приходится городить интерфейс
// DBHandler описывает методы pgc.PgHndl для мокирования в тестах
type DBHandler interface {
	Tx(ctx context.Context, cb func(ctx context.Context, tx pgx.Tx) error) error
	PgPool(ctx context.Context, cb func(ctx context.Context, p *pgxpool.Pool) error) error
}

type PgRepoDrv struct {
	shards []DBHandler
}

func newPgRepoDrv(ctx context.Context, cfg RepoDrvConfig) (RepoDrv, error) {
	slog.Info("Use PgDrv")
	shardSize := int(cfg.ShardSize())

	r := &PgRepoDrv{
		shards: make([]DBHandler, shardSize),
	}

	// В БД на текущий момент шардирования нет,
	// но ожидается горизонтальное масштабирование.
	// Поэтому в драйвере реализуем абстракцию шардирования, но на одной БД
	pgHndl, err := pgc.NewPgHndl(ctx, "PgRepoDrv", cfg.DBDSN())
	if err != nil {
		return nil, fmt.Errorf("failed create PgRepoDrv: %w", err)
	}

	for i := 0; i < shardSize; i++ {
		r.shards[i] = pgHndl
	}

	slog.Info("Use PgRepoDrv")

	return r, nil
}

// Записывает строку в указанный шард БД и возвращает строку-идентификатор записи
func (s *PgRepoDrv) UpSert(ctx context.Context, shardID byte, str string) (uint64, error) {
	if int(shardID) >= len(s.shards) {
		return 0, fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	shard := s.shards[shardID]

	sql := `
		INSERT INTO murl (url)
		VALUES ($1)
		ON CONFLICT (url)
		DO UPDATE SET url = EXCLUDED.url
		RETURNING id;
	`
	var id uint64
	err := shard.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sql, str).Scan(&id)
	})

	if err != nil {
		return 0, fmt.Errorf("Failed to execute query (%s): %w", sql, err)
	}

	return id, nil
}

func (s *PgRepoDrv) Set(ctx context.Context, shardID byte, idx uint64, u string) error {
	if int(shardID) >= len(s.shards) {
		return fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	shard := s.shards[shardID]

	sql := `
		INSERT INTO murl (id, url)
		VALUES ($1, $2)
		ON CONFLICT (id)
		DO UPDATE SET url = EXCLUDED.url;
	`
	err := shard.Tx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, idx, u)
		return err
	})

	if err != nil {
		return fmt.Errorf("Failed to execute query (%s): %w", sql, err)
	}

	return nil
}

// По строке-идентификатору возвращает ранее записанную строку
func (s *PgRepoDrv) Select(ctx context.Context, shardID byte, idx uint64) (string, error) {
	if int(shardID) >= len(s.shards) {
		return "", fmt.Errorf("shardID [%d] out of range [0, .., %d]", shardID, len(s.shards)-1)
	}

	shard := s.shards[shardID]

	sql := `
		SELECT url FROM murl
		WHERE id = $1
		LIMIT 1;
	`
	var u string
	err := shard.PgPool(ctx, func(ctx context.Context, p *pgxpool.Pool) error {
		return p.QueryRow(ctx, sql, idx).Scan(&u)
	})

	if err != nil {
		return "", fmt.Errorf("Failed to execute query (%s): %w", sql, err)
	}

	return u, nil
}
