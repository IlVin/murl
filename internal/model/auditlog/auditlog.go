package auditlog

import (
	"context"
	"log/slog"
	"murl/internal/domain"
	"sync"
)

const (
	NotificationBufSize = 1000
)

//go:generate $GOPATH/bin/mockgen -package=$GOPACKAGE -source=$GOFILE -destination=auditlog_mock_test.go

type Subscriber interface {
	Update(domain.Notification) error
	GetID() string
}

type Auditlog struct {
	mu            sync.RWMutex
	wg            sync.WaitGroup
	subscribers   map[string]Subscriber
	notifications chan domain.Notification
}

func NewAuditlog() *Auditlog {
	return &Auditlog{
		notifications: make(chan domain.Notification, NotificationBufSize),
		subscribers:   make(map[string]Subscriber),
	}
}

func (o *Auditlog) Register(s Subscriber) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.subscribers[s.GetID()] = s
	slog.Info("audit observer Register",
		slog.Any("id", s.GetID()),
	)
}

func (o *Auditlog) UnRegister(s Subscriber) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.subscribers, s.GetID())
}

func (o *Auditlog) Start(wrkCount int) {
	// Запускаем воркеров
	for range wrkCount {
		o.wg.Add(1)
		go func() {
			defer o.wg.Done()
			for n := range o.notifications {
				o.send(n)
			}
		}()
	}
	slog.Info("audit observer workers started")
}

func (o *Auditlog) Stop() {
	close(o.notifications)
	o.wg.Wait()
	slog.Info("audit observer stopped")
}

func (o *Auditlog) Notify(ctx context.Context, n domain.Notification) error {
	select {
	case o.notifications <- n:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *Auditlog) getSubscribers() []Subscriber {
	o.mu.RLock()
	defer o.mu.RUnlock()

	subs := make([]Subscriber, 0, len(o.subscribers))
	for _, s := range o.subscribers {
		subs = append(subs, s)
	}
	return subs
}

func (o *Auditlog) send(n domain.Notification) {
	for _, s := range o.getSubscribers() {
		if err := s.Update(n); err != nil {
			slog.Error("update fail", "id", s.GetID(), "err", err)
		}
	}
}
