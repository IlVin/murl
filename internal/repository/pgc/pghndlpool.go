package pgc

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrIsEmpty = errors.New("PgHndlPool is empty")
var ErrNotFound = errors.New("Not found")
var ErrNoReadyConnections = errors.New("Not found PgHndl.IsReady(true)")

type IPgHndlPool interface {
	Begin(context.Context, func(pgx.Tx) error) error
	Check(context.Context) error
	Offline()
	Size() int
	Stats() PoolStats
}

type PgHndlPool struct {
	curr uint32
	pool []*PgHndl
}

func NewPgHndlPool(hndls []*PgHndl) (*PgHndlPool, error) {
	if len(hndls) == 0 {
		return nil, ErrIsEmpty
	}
	p := &PgHndlPool{
		pool: make([]*PgHndl, len(hndls)),
	}
	copy(p.pool, hndls)

	return p, nil
}

// Begin Ищет следующий PgHndl и запускает транзакцию
func (p *PgHndlPool) Begin(ctx context.Context, cb func(pgx.Tx) error) error {
	size := uint32(len(p.pool))
	newVal := atomic.AddUint32(&p.curr, 1)

	for i := range size {
		idx := (newVal + i) % size
		h := p.pool[idx]
		if h.IsReady() {
			return h.Begin(ctx, cb)
		}
	}

	return ErrNoReadyConnections
}

// Begin Ищет следующий PgHndl и запускает проверку
func (p *PgHndlPool) Check(ctx context.Context) error {
	size := uint32(len(p.pool))
	newVal := atomic.AddUint32(&p.curr, 1)

	for i := range size {
		idx := (newVal + i) % size
		h := p.pool[idx]
		if !h.IsReady() {
			return h.Ping(ctx)
		}
	}

	return p.pool[newVal%size].Ping(ctx)
}

// Close закрывает соединения к БД всего пула
func (p *PgHndlPool) Close() {
	for _, h := range p.pool {
		h.Close()
	}
}

// Offline переводит все PgHndl в Offline состояние
func (p *PgHndlPool) Offline() {
	for _, h := range p.pool {
		h.Offline()
	}
}

func (p *PgHndlPool) Size() int {
	return len(p.pool)
}

type PoolStats struct {
	Total        int           `json:"total"`
	Online       int           `json:"online"`
	Offline      int           `json:"offline"`
	AvgLatency   time.Duration `json:"avg_latency_ns"` // В наносекундах (стандарт Go)
	MaxLatency   time.Duration `json:"max_latency_ns"`
	AvgLatencyMs int64         `json:"avg_latency_ms"` // Для удобства в JSON
	Failed       []string      `json:"failed_instances"`
}

func (p *PgHndlPool) Stats() PoolStats {
	stats := PoolStats{
		Total:  len(p.pool),
		Failed: make([]string, 0),
	}

	var totalLatency time.Duration

	for _, h := range p.pool {
		if h.IsReady() {
			stats.Online++
			l := h.Latency()
			totalLatency += l
			if l > stats.MaxLatency {
				stats.MaxLatency = l
			}
		} else {
			stats.Offline++
			stats.Failed = append(stats.Failed, h.Name())
		}
	}

	if stats.Online > 0 {
		stats.AvgLatency = totalLatency / time.Duration(stats.Online)
		stats.AvgLatencyMs = stats.AvgLatency.Milliseconds()
	}

	return stats
}
