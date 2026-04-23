// Package auditlog реализует асинхронную систему рассылки уведомлений и аудита.
// Использует паттерн "Наблюдатель" (Observer) с пулом воркеров для обеспечения
// неблокирующей обработки событий.
package auditlog

import (
	"context"
	"log/slog"
	"murl/internal/domain"
	"sync"
)

const (
	// NotificationBufSize определяет емкость канала уведомлений.
	// Позволяет сглаживать пиковые нагрузки на систему аудита.
	NotificationBufSize = 1000
)

//go:generate $GOPATH/bin/mockgen -package=$GOPACKAGE -source=$GOFILE -destination=auditlog_mock_test.go

// Subscriber определяет интерфейс для потребителей уведомлений аудита.
type Subscriber interface {
	Update(domain.Notification) error
	GetID() string
}

// Auditlog управляет регистрацией подписчиков и распределением уведомлений между ними.
// Поддерживает безопасную конкурентную работу и гарантированное завершение (Graceful Shutdown).
type Auditlog struct {
	mu            sync.RWMutex
	wg            sync.WaitGroup
	subscribers   map[string]Subscriber
	notifications chan domain.Notification
}

// NewAuditlog создает новый экземпляр системы аудита с инициализированными буферами.
func NewAuditlog() *Auditlog {
	return &Auditlog{
		notifications: make(chan domain.Notification, NotificationBufSize),
		subscribers:   make(map[string]Subscriber),
	}
}

// Register добавляет нового подписчика в список рассылки.
// Если подписчик с таким ID уже существует, он будет перезаписан.
func (o *Auditlog) Register(s Subscriber) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.subscribers[s.GetID()] = s
	slog.Info("audit observer Register",
		slog.Any("id", s.GetID()),
	)
}

// UnRegister удаляет подписчика из списка рассылки по его ID.
func (o *Auditlog) UnRegister(s Subscriber) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.subscribers, s.GetID())
}

// Start запускает указанное количество фоновых воркеров для обработки очереди уведомлений.
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

// Stop инициирует корректное завершение работы: закрывает очередь и дожидается,
// пока все воркеры отправят текущие уведомления.
func (o *Auditlog) Stop() {
	close(o.notifications)
	o.wg.Wait()
	slog.Info("audit observer stopped")
}

// Notify помещает уведомление в очередь на отправку.
// Метод является неблокирующим до тех пор, пока буфер канала не переполнен.
// Возвращает ошибку, если контекст отменен до того, как удалось поместить сообщение в очередь.
func (o *Auditlog) Notify(ctx context.Context, n domain.Notification) error {
	select {
	case o.notifications <- n:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getSubscribers возвращает срез текущих подписчиков, используя RLock для безопасного доступа.
func (o *Auditlog) getSubscribers() []Subscriber {
	o.mu.RLock()
	defer o.mu.RUnlock()

	subs := make([]Subscriber, 0, len(o.subscribers))
	for _, s := range o.subscribers {
		subs = append(subs, s)
	}
	return subs
}

// send выполняет последовательную рассылку конкретного уведомления всем подписчикам.
func (o *Auditlog) send(n domain.Notification) {
	for _, s := range o.getSubscribers() {
		if err := s.Update(n); err != nil {
			slog.Error("update fail", "id", s.GetID(), "err", err)
		}
	}
}
