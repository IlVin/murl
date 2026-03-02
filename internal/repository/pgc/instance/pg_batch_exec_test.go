package instance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"murl/internal/mocks"          // путь к сгенерированным мокам
	"murl/internal/repository/pgc" // путь к вашим интерфейсам

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestBatchExec_Add_TriggerFlush(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPg := mocks.NewMockPgInstance(ctrl)
	wg := &sync.WaitGroup{}

	// Ожидаем, что транзакция вызовется один раз, когда наберется BatchBufSize
	mockPg.EXPECT().
		Tx(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, f func(context.Context, pgc.PgxTxIface) error) error {
			return nil // Симулируем успех
		}).Times(1)

	be := NewBatchExec(mockPg, nil, wg)

	// Добавляем BatchBufSize элементов
	for i := 0; i < BatchBufSize; i++ {
		be.Add(BatchQuery{SQL: "INSERT INTO test (id) VALUES ($1)", Params: []any{i}})
	}

	wg.Wait() // Ждем выполнения горутины
}

func TestBatchExec_Flush_Manual(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPg := mocks.NewMockPgInstance(ctrl)
	mockTx := mocks.NewMockPgxTxIface(ctrl)
	mockBr := mocks.NewMockBatchResults(ctrl) // Нужен мок для pgx.BatchResults

	wg := &sync.WaitGroup{}
	be := NewBatchExec(mockPg, nil, wg)

	// 1. Ожидаем вызов транзакции
	mockPg.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, f func(context.Context, pgc.PgxTxIface) error) error {
			return f(ctx, mockTx)
		})

	// 2. Ожидаем отправку батча
	mockTx.EXPECT().SendBatch(gomock.Any(), gomock.Any()).Return(mockBr)

	// 3. Ожидаем чтение результатов (1 запрос)
	mockBr.EXPECT().Exec().Return(pgconn.CommandTag{}, nil).Times(1)
	mockBr.EXPECT().Close().Return(nil)

	be.Add(BatchQuery{SQL: "SELECT 1"})
	be.Flush()

	wg.Wait()
}

func TestBatchExec_TransactionError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPg := mocks.NewMockPgInstance(ctrl)
	wg := &sync.WaitGroup{}
	be := NewBatchExec(mockPg, nil, wg)

	// Имитируем ошибку в транзакции
	mockPg.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(errors.New("db connection lost"))

	be.Add(BatchQuery{SQL: "UPDATE users SET age = 1"})
	be.Flush()

	wg.Wait()
	// Проверяем логи визуально или через кастомный логгер.
	// Главное — код не упал и горутина завершилась (Done вызвался).
}

func TestBatchExec_LimiterBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPg := mocks.NewMockPgInstance(ctrl)
	// Имитируем долгую транзакцию
	mockPg.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, f func(context.Context, pgc.PgxTxIface) error) error {
			time.Sleep(100 * time.Millisecond)
			return nil
		}).AnyTimes()

	// Лимитер на 1 батч
	limiter := make(chan bool, 1)
	wg := &sync.WaitGroup{}
	be := NewBatchExec(mockPg, limiter, wg)

	// Запускаем два батча
	be.Add(BatchQuery{SQL: "Q1"})
	be.Flush()
	be.Add(BatchQuery{SQL: "Q2"})
	be.Flush()

	// ДАЕМ ВРЕМЯ горутинам захватить лимитер
	time.Sleep(20 * time.Millisecond)

	// В этот момент в канале limiter должен быть 1 токен (занят первым батчем)
	// Второй батч висит и ждет
	assert.Len(t, limiter, 1)

	wg.Wait()
	assert.Len(t, limiter, 0)
}
