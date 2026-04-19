package pgbatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"murl/internal/repository/pgc"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// Константы для тестов, если они не экспортированы из твоего пакета
const (
	testBatchBufSize = 2
	testBatchLimit   = 2
)

func TestPgBatchRowQuery_AllScenarios(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPg1 := NewMockPgInstance(ctrl)
	mockPg2 := NewMockPgInstance(ctrl)
	mockTx := NewMockPgxTxIface(ctrl)
	mockBR := NewMockBatchResults(ctrl)
	mockRow := NewMockRow(ctrl)

	// Настройка базового поведения String() для шардирования
	mockPg1.EXPECT().String().Return("db-1").AnyTimes()
	mockPg2.EXPECT().String().Return("db-2").AnyTimes()

	t.Run("Success Batch by Size and Metadata preservation", func(t *testing.T) {
		batcher := NewPgBatchRowQuery()

		// Ожидаем выполнение 1 транзакции для 2 запросов (размер батча)
		mockPg1.EXPECT().
			Tx(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, f func(context.Context, pgc.PgxTxIface) error) error {
				return f(ctx, mockTx)
			})

		mockTx.EXPECT().SendBatch(gomock.Any(), gomock.Any()).Return(mockBR)
		mockBR.EXPECT().Close().Return(nil)
		mockBR.EXPECT().QueryRow().Return(mockRow).Times(testBatchBufSize)
		mockRow.EXPECT().Scan(gomock.Any()).Return(nil).Times(testBatchBufSize)

		// Пушим 2 запроса с Metadata
		metadata := "coord-123"
		batcher.Push(mockPg1, &BRQuery{SQL: "SELECT 1", Metadata: metadata})
		batcher.Push(mockPg1, &BRQuery{SQL: "SELECT 2", Metadata: "other-meta"})

		// Проверяем первый результат
		res := <-batcher.C()
		assert.Equal(t, metadata, res.Metadata)
		assert.NoError(t, res.Err)

		<-batcher.C() // вычитываем второй
		batcher.Close()
	})

	t.Run("Flush by Timeout", func(t *testing.T) {
		batcher := NewPgBatchRowQuery()

		mockPg1.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(nil)

		// Пушим только 1 запрос (лимит батча 2, так что по размеру не сработает)
		batcher.Push(mockPg1, &BRQuery{Metadata: "timeout-task"})

		// Ждем срабатывания тикера (BatchTimeout = 500ms)
		start := time.Now()
		select {
		case res := <-batcher.C():
			assert.Equal(t, "timeout-task", res.Metadata)
			assert.WithinDuration(t, start.Add(BatchTimeout), time.Now(), 200*time.Millisecond)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout flush failed")
		}
		batcher.Close()
	})

	t.Run("Sharding - Multiple DBs", func(t *testing.T) {
		batcher := NewPgBatchRowQuery()

		// Ожидаем две разные транзакции для двух разных шардов
		mockPg1.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(nil)
		mockPg2.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(nil)

		batcher.Push(mockPg1, &BRQuery{Metadata: "db1-query"})
		batcher.Push(mockPg2, &BRQuery{Metadata: "db2-query"})

		batcher.Close() // Вызовет Flush для обоих шардов

		results := make([]any, 0)
		for res := range batcher.C() {
			results = append(results, res.Metadata)
		}
		assert.Len(t, results, 2)
		assert.Contains(t, results, "db1-query")
		assert.Contains(t, results, "db2-query")
	})

	t.Run("Database Transaction Error", func(t *testing.T) {
		batcher := NewPgBatchRowQuery()
		dbErr := errors.New("connection reset")

		mockPg1.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(dbErr)

		batcher.Push(mockPg1, &BRQuery{Metadata: "error-task"})
		batcher.Close()

		res := <-batcher.C()
		assert.ErrorIs(t, res.Err, dbErr)
	})

	t.Run("Panic Recovery in Query", func(t *testing.T) {
		batcher := NewPgBatchRowQuery()

		// Имитируем панику внутри Tx
		mockPg1.EXPECT().Tx(gomock.Any(), gomock.Any()).Do(func(any, any) {
			panic("unexpected memory crash")
		})

		batcher.Push(mockPg1, &BRQuery{Metadata: "panic-task"})
		batcher.Close()

		// Канал chOut должен закрыться корректно даже после паники в горутине query
		_, ok := <-batcher.C()
		assert.False(t, ok, "Output channel should be closed")
	})
}
