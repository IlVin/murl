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

// TestComplexBatcher_RetryLogic тестирует логику ретраев для Query-батчера
func TestComplexBatcher_RetryLogic(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockMaster := NewMockPgInstance(ctrl)

	type User struct{ ID int }
	readQuery := NewQuery("SELECT 1", func(u *User) []any { return []any{&u.ID} }).AsRead()

	batcher := NewPgBatcher[int](ctx, mockMaster, readQuery)

	t.Run("Retry Logic: Network Error then Success", func(t *testing.T) {
		netErr := mockNetError{error: fmt.Errorf("network timeout")}

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

		done := make(chan struct{})
		var finalID int
		var finalErr error

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

		batcher.Requests() <- BatchEntry[int]{
			Args: []any{1},
			Ctx:  100,
		}

		err := batcher.Close()
		require.NoError(t, err)

		select {
		case <-done:
			assert.Equal(t, 42, finalID)
			assert.NoError(t, finalErr)
		case <-time.After(2 * time.Second):
			t.Fatal("test timed out waiting for batcher results")
		}
	})
}
