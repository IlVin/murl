package pgc

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

var ErrAlreadyRunning = errors.New("CheckService already running")
var ErrAlreadyStop = errors.New("CheckService already stop")
var ErrInvalidInstance = errors.New("CheckService instance is invalid")

type CheckService struct {
	mu        sync.RWMutex
	wg        sync.WaitGroup
	cancelFn  context.CancelFunc
	isRunning atomic.Bool
	instances map[string]IPgHndlPool
}

func NewCheckService(hndlrs []*PgHndl) (*CheckService, error) {
	c := &CheckService{
		wg:        sync.WaitGroup{},
		instances: make(map[string]IPgHndlPool),
	}

	// Сортируем PgHndl по инстансам
	mHndls := make(map[string][]*PgHndl)
	for _, h := range hndlrs {
		instance := h.Host()
		if instance == "" {
			return nil, ErrInvalidInstance
		}
		if _, ok := mHndls[instance]; !ok {
			mHndls[instance] = make([]*PgHndl, 0, 10)
		}
		mHndls[instance] = append(mHndls[instance], h)
	}

	// Инициализируем пулы
	for host, hndls := range mHndls {
		hPool, err := NewPgHndlPool(hndls)
		if err != nil {
			slog.Error(
				"Cannot create HndlPool",
				slog.String("instance", host),
				slog.Any("err", err),
			)
			continue
		}
		c.instances[host] = hPool
	}

	return c, nil
}

func (s *CheckService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning.Load() {
		return ErrAlreadyRunning
	}
	ctx, s.cancelFn = context.WithCancel(ctx)
	s.isRunning.Store(true)

	for instance, p := range s.instances {
		slog.Info(
			"Start PgHndlPool checking",
			slog.String("instance", instance),
		)
		s.wg.Add(1)
		go s.StartPool(ctx, instance, p)
	}
	return nil
}

func (s *CheckService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning.Load() {
		return ErrAlreadyStop
	}
	s.isRunning.Store(false)
	s.cancelFn()
	s.wg.Wait()

	return nil
}

func (s *CheckService) StartPool(ctx context.Context, instance string, p IPgHndlPool) {
	defer s.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error(
				"CRITICAL PANIC RECOVERED",
				slog.String("component", "CheckService"),
				slog.Any("panic", r),
				slog.String("instance", instance),
				slog.String("stack", string(debug.Stack())),
			)

			if ctx.Err() == nil {
				slog.Info(
					"Restarting worker after panic",
					slog.String("instance", instance),
				)
				go func() {
					select {
					case <-time.After(time.Second):
						if s.isRunning.Load() {
							s.wg.Add(1)
							s.StartPool(ctx, instance, p)
						} else {
							slog.Info(
								"The worker was not restarted after panic due to a service stop.",
								slog.String("instance", instance),
							)
						}
					case <-ctx.Done():
						return // Не рестартуем, если контекст закрыт
					}
				}()
			}
		}
	}()

	timer := time.NewTicker(time.Duration(max(2000/max(p.Size(), 1), 100)) * time.Millisecond)
	defer timer.Stop()

	slog.Info(
		"Start worker",
		slog.String("component", "CheckService"),
		slog.String("instance", instance),
	)

	for s.isRunning.Load() {
		select {
		case <-ctx.Done():
			slog.Info("Stop worker",
				slog.String("component", "CheckService"),
				slog.String("instance", instance),
			)
			return
		case <-timer.C:
			err := p.Check(ctx)
			if err != nil {
				if IsNetworkError(err) {
					p.Offline()
				}
			}
		}
	}
}

func (s *CheckService) GetStats() map[string]PoolStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make(map[string]PoolStats, len(s.instances))
	for host, pool := range s.instances {
		res[host] = pool.Stats()
	}
	return res
}
