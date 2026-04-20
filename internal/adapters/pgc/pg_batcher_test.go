package pgc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	traceNoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"
)

func TestComplexBatcherWithCQRS(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	mockMaster := NewMockPgInstance(ctrl)
	mockReplica := NewMockPgInstance(ctrl)

	mockMaster.EXPECT().String().Return("master:5432").AnyTimes()
	mockMaster.EXPECT().WithTracerProvider(gomock.Any()).Return(mockMaster).AnyTimes()
	mockMaster.EXPECT().WithMeterProvider(gomock.Any()).Return(mockMaster).AnyTimes()

	mockReplica.EXPECT().String().Return("replica:5432").AnyTimes()
	mockReplica.EXPECT().IsOnline().Return(true).AnyTimes()
	mockReplica.EXPECT().WithTracerProvider(gomock.Any()).Return(mockReplica).AnyTimes()
	mockReplica.EXPECT().WithMeterProvider(gomock.Any()).Return(mockReplica).AnyTimes()

	cqrs := NewCQRSConnector(mockMaster, mockReplica)

	t.Run("Retry Logic: First Node Fails", func(t *testing.T) {
		readQuery := NewQuery("SELECT 1", func(u *int) []any { return []any{u} }).AsRead()
		// Имитируем поведение PgClusterBatcher
		readBatcher := NewPgBatcher(ctx, cqrs, readQuery).WithMeterProvider(noop.NewMeterProvider())

		netErr := mockNetError{error: fmt.Errorf("network timeout")}

		// 1. Ошибка на реплике
		mockReplica.EXPECT().
			SendBatch(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(func(yield func(any, error) bool) {
				var zero any
				yield(zero, netErr)
			})

		// 2. Успех на мастере
		mockMaster.EXPECT().
			SendBatch(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(func(yield func(any, error) bool) {
				val := 1
				yield(&val, nil)
			})

		err := readBatcher.Push(1)
		require.NoError(t, err)

		results := make(chan int, 1)
		go func() {
			for val, err := range readBatcher.All() {
				if err == nil {
					results <- val
				}
			}
			close(results)
		}()

		err = readBatcher.Flush()
		assert.NoError(t, err)

		_ = readBatcher.Close()
		assert.Equal(t, 1, <-results)
	})
}

func TestPgBatcher_With_PgConnector(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	mockPool := NewMockPgxPoolIface(ctrl)
	mockBR := NewMockBatchResults(ctrl)
	mockRows := NewMockRows(ctrl)

	meter := noop.NewMeterProvider().Meter("test")
	mLatency, _ := meter.Float64Histogram("db.pgc.operation.duration")

	conn := &pgConnector{
		pool:     mockPool,
		tracer:   traceNoop.NewTracerProvider().Tracer("test"),
		meter:    meter,
		mLatency: mLatency,
		cb:       gobreaker.NewCircuitBreaker(gobreaker.Settings{}),
		now:      time.Now,
		host:     "localhost",
		database: "testdb",
	}
	conn.isOnline.Store(1)

	type User struct{ ID int }
	query := NewQuery("SELECT id FROM users WHERE id = $1", func(u *User) []any {
		return []any{&u.ID}
	})
	batcher := NewPgBatcher(ctx, conn, query)

	t.Run("Full pipeline: Batcher -> Connector -> MockPool", func(t *testing.T) {
		mockPool.EXPECT().
			SendBatch(gomock.Any(), gomock.Any()).
			Return(mockBR)

		mockBR.EXPECT().Query().Return(mockRows, nil)

		mockRows.EXPECT().Next().Return(true)
		mockRows.EXPECT().Scan(gomock.Any()).DoAndReturn(func(dest ...any) error {
			// dest[0] — это указатель на ID (&u.ID)
			*(dest[0].(*int)) = 42
			return nil
		})
		mockRows.EXPECT().Next().Return(false)

		mockRows.EXPECT().Err().Return(nil).AnyTimes()
		mockRows.EXPECT().Close().AnyTimes()
		mockBR.EXPECT().Close().AnyTimes()

		_ = batcher.Push(1)

		resultsChan := make(chan int, 1)
		go func() {
			for res, err := range batcher.All() {
				if err == nil {
					resultsChan <- res.ID
				}
			}
			close(resultsChan)
		}()

		err := batcher.Flush()
		require.NoError(t, err)
		err = batcher.Close()
		require.NoError(t, err)

		assert.Equal(t, 42, <-resultsChan)
	})
}
