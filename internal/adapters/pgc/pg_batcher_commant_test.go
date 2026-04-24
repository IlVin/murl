package pgc

import (
	"context"
	"fmt"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// TestPgBatcher_CommandMode тестирует Command-батчер (запросы без возврата данных)
func TestPgBatcher_CommandMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	mockPg := NewMockPgInstance(ctrl)

	cmd := NewCommand("UPDATE users SET active = true")

	t.Run("Should send error on failure", func(t *testing.T) {
		expectedErr := fmt.Errorf("batch execution failed")

		mockPg.EXPECT().
			SendBatch(gomock.Any(), cmd, gomock.Any()).
			DoAndReturn(func(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					yield(nil, expectedErr)
				}
			}).
			Times(1)

		batcher := NewPgBatcher[string](ctx, mockPg, cmd)

		go func() {
			defer batcher.Close()
			batcher.Requests() <- BatchEntry[string]{Args: []any{}, Ctx: "task-1"}
			batcher.Requests() <- BatchEntry[string]{Args: []any{}, Ctx: "task-2"}
		}()

		var receivedError error
		for res := range batcher.Results() {
			receivedError = res.Err
		}

		assert.Error(t, receivedError)
		assert.Contains(t, receivedError.Error(), "batch failed")
	})

	t.Run("Should send no results on success", func(t *testing.T) {
		mockPg.EXPECT().
			SendBatch(gomock.Any(), cmd, gomock.Any()).
			DoAndReturn(func(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					// Ничего не отправляем - успешное выполнение
				}
			}).
			Times(1)

		batcher := NewPgBatcher[string](ctx, mockPg, cmd)

		go func() {
			defer batcher.Close()
			batcher.Requests() <- BatchEntry[string]{Args: []any{}, Ctx: "task-1"}
			batcher.Requests() <- BatchEntry[string]{Args: []any{}, Ctx: "task-2"}
		}()

		resultCount := 0
		for range batcher.Results() {
			resultCount++
		}

		assert.Equal(t, 0, resultCount, "При успехе не должно быть результатов")
	})

	t.Run("Should handle no requests gracefully", func(t *testing.T) {
		batcher := NewPgBatcher[string](ctx, mockPg, cmd)

		err := batcher.Close()
		assert.NoError(t, err)

		resultCount := 0
		for range batcher.Results() {
			resultCount++
		}
		assert.Equal(t, 0, resultCount)
	})
}
