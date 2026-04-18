package auditlog

import (
	"context"
	"errors"
	"murl/internal/domain"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
)

func TestAuditlog_RegisterUnregister(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	o := NewAuditlog()
	sub := NewMockSubscriber(ctrl)
	subID := "sub-1"

	sub.EXPECT().GetID().Return(subID).AnyTimes()

	// Тест регистрации
	o.Register(sub)
	subs := o.getSubscribers()
	require.Len(t, subs, 1)
	assert.Equal(t, sub, subs[0])

	// Тест удаления
	o.UnRegister(sub)
	assert.Empty(t, o.getSubscribers())
}

func TestAuditlog_Lifecycle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	o := NewAuditlog()
	n := domain.Notification{Message: []byte("hello")}
	sub := NewMockSubscriber(ctrl)

	sub.EXPECT().GetID().Return("sub-1").AnyTimes()
	// Ожидаем один успешный вызов
	sub.EXPECT().Update(n).Return(nil).Times(1)

	o.Register(sub)
	o.Start(2) // Запускаем 2 воркера

	err := o.Notify(context.Background(), n)
	assert.NoError(t, err)

	// Небольшая пауза, чтобы воркер успел вычитать из канала
	time.Sleep(50 * time.Millisecond)
	o.Stop()
}

func TestAuditlog_NotifyContextTimeout(t *testing.T) {
	// Создаем обсервер с буфером 1, чтобы быстро его забить
	o := &Auditlog{
		notifications: make(chan domain.Notification, 1),
		subscribers:   make(map[string]Subscriber),
	}

	n := domain.Notification{Message: []byte("blocker")}

	// Забиваем единственный слот в буфере
	o.notifications <- n

	// Пытаемся отправить второе уведомление с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := o.Notify(ctx, n)

	// Проверяем, что получили ошибку дедлайна, а не зависли
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestAuditlog_SendErrorLogging(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	o := NewAuditlog()
	sub := NewMockSubscriber(ctrl)
	n := domain.Notification{Message: []byte("fail")}

	sub.EXPECT().GetID().Return("bad-sub").AnyTimes()
	// Возвращаем ошибку, чтобы покрыть ветку slog.Error в методе send()
	sub.EXPECT().Update(n).Return(errors.New("network error")).Times(1)

	o.Register(sub)

	// Вызываем напрямую send, чтобы не возиться с таймингами воркеров
	o.send(n)
}

func TestAuditlog_Concurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	o := NewAuditlog()
	sub := NewMockSubscriber(ctrl)
	const count = 50

	sub.EXPECT().GetID().Return("multi-sub").AnyTimes()
	sub.EXPECT().Update(gomock.Any()).Return(nil).Times(count)

	o.Register(sub)
	o.Start(10) // 10 воркеров

	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			_ = o.Notify(context.Background(), domain.Notification{Message: []byte("msg")})
		})
	}

	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	o.Stop()
}

func TestAuditlog_GetSubscribersSafety(t *testing.T) {
	// Проверяем, что getSubscribers возвращает копию и не падает при пустой мапе
	o := NewAuditlog()
	subs := o.getSubscribers()
	assert.NotNil(t, subs)
	assert.Len(t, subs, 0)
}
