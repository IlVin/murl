package pgc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestComplexBatcher_RetryLogic(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockMaster := NewMockPgInstance(ctrl)

	type User struct{ ID int }
	// Используем конкретный тип в NewQuery для соответствия итератору
	readQuery := NewQuery("SELECT 1", func(u *User) []any { return []any{&u.ID} }).AsRead()

	batcher := NewPgBatcher[int](ctx, mockMaster, readQuery)

	t.Run("Retry Logic: Network Error then Success", func(t *testing.T) {
		netErr := mockNetError{error: fmt.Errorf("network timeout")}

		// Настраиваем ожидания мока
		gomock.InOrder(
			mockMaster.EXPECT().
				SendBatch(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(func(yield func(any, error) bool) {
					yield(nil, netErr)
				}),

			mockMaster.EXPECT().
				SendBatch(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(func(yield func(any, error) bool) {
					val := &User{ID: 42}
					yield(val, nil)
				}),
		)

		// Канал для сбора финального результата
		done := make(chan struct{})
		var finalID int
		var finalErr error

		// Читаем результаты. Цикл завершится, когда batcher закроет Results() внутри Stop/worker
		go func() {
			for res := range batcher.Results() {
				if res.Err != nil {
					finalErr = res.Err
				} else {
					finalID = res.Data.ID
				}
			}
			close(done)
		}()

		// Отправляем задачу
		batcher.Requests() <- BatchEntry[int]{
			Args: []any{1},
			Ctx:  100,
		}

		// Закрываем батчер. Это инициирует flush, дождется воркера и закроет канал Results.
		err := batcher.Close()
		require.NoError(t, err)

		// Ждем завершения чтения результатов
		select {
		case <-done:
			assert.Equal(t, 42, finalID)
			assert.NoError(t, finalErr)
		case <-time.After(2 * time.Second):
			t.Fatal("test timed out waiting for batcher results")
		}
	})
}

func TestPgBatcher_CommandMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	mockPg := NewMockPgInstance(ctrl)

	// Режим Command (binder == nil)
	cmd := NewCommand("UPDATE users SET active = true")
	batcher := NewPgBatcher[string](ctx, mockPg, cmd)

	t.Run("Should execute without sending results", func(t *testing.T) {
		mockPg.EXPECT().
			SendBatch(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(func(yield func(any, error) bool) {
				// В режиме Command yield вызывается, но val не используется
				yield(nil, nil)
			})

		go func() {
			defer batcher.Close()
			batcher.Requests() <- BatchEntry[string]{Args: []any{}, Ctx: "task-1"}
		}()

		// В режиме Command канал Results закроется, ничего не прислав
		for res := range batcher.Results() {
			t.Errorf("expected no results for command, got %+v", res)
		}
	})
}
