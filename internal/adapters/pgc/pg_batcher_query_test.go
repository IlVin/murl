package pgc

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestPgBatcher_DataLossDemo демонстрирует отсутствие потери данных в Query-батчере
func TestPgBatcher_DataLossDemo(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	mockMaster := NewMockPgInstance(ctrl)

	type User struct {
		ID   int
		Name string
	}

	query := NewQuery("SELECT id, name FROM users", func(u *User) []any {
		return []any{&u.ID, &u.Name}
	})

	batcher := NewPgBatcher[int](ctx, mockMaster, query)

	const numRequests = 500

	expectedCalls := numRequests / DefaultBatchLimit
	if numRequests%DefaultBatchLimit != 0 {
		expectedCalls++
	}

	t.Logf("Ожидается %d вызовов SendBatch", expectedCalls)

	var totalSent atomic.Int32
	var totalReceived atomic.Int32

	mockMaster.EXPECT().
		SendBatch(gomock.Any(), query, gomock.Any()).
		DoAndReturn(func(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
			batchSize := len(args)
			t.Logf("SendBatch вызван с %d запросами", batchSize)

			return func(yield func(any, error) bool) {
				for i := 1; i <= batchSize; i++ {
					sent := totalSent.Add(1)
					if !yield(&User{ID: int(sent), Name: fmt.Sprintf("User%d", sent)}, nil) {
						t.Logf("Итератор прерван на результате %d", sent)
						return
					}
				}
				t.Logf("Batch из %d результатов отправлен", batchSize)
			}
		}).
		Times(expectedCalls)

	go func() {
		defer func() {
			if err := batcher.Close(); err != nil {
				slog.Error("batcher close fail",
					slog.Any("err", err),
				)
			}
		}()
		for i := 1; i <= numRequests; i++ {
			batcher.Requests() <- BatchEntry[int]{
				Args: []any{i, fmt.Sprintf("User%d", i)},
				Ctx:  i,
			}
		}
	}()

	for range batcher.Results() {
		totalReceived.Add(1)
	}

	t.Logf("РЕЗУЛЬТАТЫ ТЕСТА")
	t.Logf("Отправлено запросов: %d", numRequests)
	t.Logf("SendBatch отправил результатов: %d", totalSent.Load())
	t.Logf("Потребитель получил результатов: %d", totalReceived.Load())

	if totalReceived.Load() != totalSent.Load() {
		t.Errorf("ПРОБЛЕМА ОБНАРУЖЕНА: отправлено %d, получено %d. Потеряно %d результатов",
			totalSent.Load(), totalReceived.Load(), totalSent.Load()-totalReceived.Load())
	} else {
		t.Logf("Потерь данных нет")
	}
}

// TestPgBatcher_SlowConsumerNoDataLoss тестирует отсутствие потери данных с медленным потребителем
func TestPgBatcher_SlowConsumerNoDataLoss(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	mockMaster := NewMockPgInstance(ctrl)

	type User struct {
		ID   int
		Name string
	}

	query := NewQuery("SELECT id, name FROM users", func(u *User) []any {
		return []any{&u.ID, &u.Name}
	})

	batcher := NewPgBatcher[int](ctx, mockMaster, query)

	const numRequests = 200
	const batchSize = 50
	expectedCalls := numRequests / batchSize

	var totalSent atomic.Int32
	var totalReceived atomic.Int32
	var yieldInterrupted atomic.Bool

	mockMaster.EXPECT().
		SendBatch(gomock.Any(), query, gomock.Any()).
		DoAndReturn(func(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				for i := 1; i <= len(args); i++ {
					sent := totalSent.Add(1)
					if !yield(&User{ID: int(sent), Name: fmt.Sprintf("User%d", sent)}, nil) {
						t.Logf("Итератор прерван на результате %d", sent)
						yieldInterrupted.Store(true)
						return
					}
				}
			}
		}).
		Times(expectedCalls)

	go func() {
		defer func() {
			if err := batcher.Close(); err != nil {
				slog.Error("batcher close fail",
					slog.Any("err", err),
				)
			}
		}()
		for i := 1; i <= numRequests; i++ {
			batcher.Requests() <- BatchEntry[int]{
				Args: []any{i, fmt.Sprintf("User%d", i)},
				Ctx:  i,
			}
		}
	}()

	results := make([]BatchResult[User, int], 0)

	for res := range batcher.Results() {
		totalReceived.Add(1)
		results = append(results, res)
		time.Sleep(1 * time.Millisecond)
	}

	t.Logf("ИТОГИ ТЕСТА")
	t.Logf("Отправлено запросов: %d", numRequests)
	t.Logf("SendBatch отправил результатов: %d", totalSent.Load())
	t.Logf("Потребитель получил результатов: %d", totalReceived.Load())
	t.Logf("Итератор был прерван: %v", yieldInterrupted.Load())

	assert.Equal(t, numRequests, len(results),
		"Должны быть получены все %d результатов, получено %d", numRequests, len(results))

	receivedIDs := make(map[int]bool)
	for _, res := range results {
		receivedIDs[res.Data.ID] = true
	}

	for i := 1; i <= numRequests; i++ {
		assert.True(t, receivedIDs[i], "ID %d должен быть получен", i)
	}

	t.Logf("Все %d результатов успешно доставлены", len(results))
}

// TestPgBatcher_MultipleBatchesNoDataLoss тестирует корректную обработку нескольких батчей
func TestPgBatcher_MultipleBatchesNoDataLoss(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()
	mockMaster := NewMockPgInstance(ctrl)

	type Item struct {
		Value int
	}

	query := NewQuery("SELECT value FROM items", func(i *Item) []any {
		return []any{&i.Value}
	})

	batcher := NewPgBatcher[int](ctx, mockMaster, query)

	const numRequests = 150
	const batchSize = 50
	expectedCalls := numRequests / batchSize

	var allSent int32
	var sentIds []int
	var mu sync.Mutex

	mockMaster.EXPECT().
		SendBatch(gomock.Any(), query, gomock.Any()).
		DoAndReturn(func(ctx context.Context, q PgQuery, args [][]any) iter.Seq2[any, error] {
			return func(yield func(any, error) bool) {
				for i := 1; i <= len(args); i++ {
					id := int(atomic.AddInt32(&allSent, 1))
					mu.Lock()
					sentIds = append(sentIds, id)
					mu.Unlock()

					if !yield(&Item{Value: id}, nil) {
						return
					}
				}
			}
		}).
		Times(expectedCalls)

	go func() {
		defer func() {
			if err := batcher.Close(); err != nil {
				slog.Error("batcher close fail",
					slog.Any("err", err),
				)
			}
		}()
		for i := 1; i <= numRequests; i++ {
			batcher.Requests() <- BatchEntry[int]{
				Args: []any{i},
				Ctx:  i,
			}
		}
	}()

	received := make(map[int]int)
	for res := range batcher.Results() {
		require.NoError(t, res.Err)
		received[res.Ctx] = res.Data.Value
	}

	t.Logf("Отправлено запросов: %d", numRequests)
	t.Logf("SendBatch суммарно отправил: %d результатов", len(sentIds))
	t.Logf("Получено результатов: %d", len(received))

	assert.Equal(t, numRequests, len(received),
		"Должны быть получены все %d результатов", numRequests)

	for i := 1; i <= numRequests; i++ {
		value, ok := received[i]
		assert.True(t, ok, "Результат для запроса %d не получен", i)
		assert.Equal(t, i, value, "Значение для запроса %d должно быть %d, получено %d", i, i, value)
	}

	t.Logf("Все %d результатов успешно доставлены с правильной привязкой к Ctx", numRequests)
}
