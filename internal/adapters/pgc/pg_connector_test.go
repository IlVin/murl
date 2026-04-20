package pgc

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	pgconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metricNoop "go.opentelemetry.io/otel/metric/noop"
	traceNoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"
)

// setupConnector инициализирует коннектор с noop телеметрией и моком пула
func setupConnector(t *testing.T, pool PgxPoolIface) *pgConnector {
	// Используем стандартные Noop провайдеры
	tp := traceNoop.NewTracerProvider()
	mp := metricNoop.NewMeterProvider()

	p := &pgConnector{
		host:     "localhost",
		port:     5432,
		database: "test",
		pool:     pool,
		tracer:   tp.Tracer("pgc_test"),
		meter:    mp.Meter("pgc_test"),
		logger:   slog.Default(),
		now:      time.Now,
	}
	p.isOnline.Store(1)

	// Инициализируем метрику, чтобы mLatency не был nil
	p.mLatency, _ = p.meter.Float64Histogram("db.pgc.operation.duration")

	// Настраиваем CB с малым порогом для тестов
	p.cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        p.String(),
		MaxRequests: 1,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 3 },
		OnStateChange: func(_ string, _, to gobreaker.State) {
			if to == gobreaker.StateOpen {
				p.isOnline.Store(0)
			} else {
				p.isOnline.Store(1)
			}
		},
	})

	return p
}

func TestPgConnector_FetchRow(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := NewMockPgxPoolIface(ctrl)
	mockQuery := NewMockPgQuery(ctrl)
	p := setupConnector(t, mockPool)

	t.Run("success", func(t *testing.T) {
		type res struct{ ID int }
		target := &res{}

		mockQuery.EXPECT().Name().Return("TestQuery").AnyTimes()
		mockQuery.EXPECT().SQL().Return("SELECT 1").AnyTimes()
		mockQuery.EXPECT().NewTarget().Return(target)
		mockQuery.EXPECT().Binder(target).Return([]any{&target.ID})

		mockRow := NewMockRow(ctrl)
		mockPool.EXPECT().QueryRow(gomock.Any(), "SELECT 1").Return(mockRow)
		mockRow.EXPECT().Scan(gomock.Any()).Return(nil)

		resVal, err := p.FetchRow(ctx, mockQuery)
		assert.NoError(t, err)
		assert.Equal(t, target, resVal)
	})
}

func TestPgConnector_Fetch_Iterator(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := NewMockPgxPoolIface(ctrl)
	mockQuery := NewMockPgQuery(ctrl)
	p := setupConnector(t, mockPool)

	t.Run("yields multiple rows", func(t *testing.T) {
		mockQuery.EXPECT().Name().Return("TestStream").AnyTimes()
		mockQuery.EXPECT().SQL().Return("SELECT id").AnyTimes()

		mockRows := NewMockRows(ctrl)
		mockPool.EXPECT().Query(gomock.Any(), "SELECT id").Return(mockRows, nil)

		// Эмулируем 2 строки
		mockRows.EXPECT().Next().Return(true).Times(2)
		mockRows.EXPECT().Next().Return(false)
		mockRows.EXPECT().Scan(gomock.Any()).Return(nil).Times(2)
		mockRows.EXPECT().Err().Return(nil).AnyTimes()
		mockRows.EXPECT().Close().AnyTimes()

		mockQuery.EXPECT().NewTarget().Return(&struct{ ID int }{}).Times(2)
		mockQuery.EXPECT().Binder(gomock.Any()).Return([]any{new(int)}).Times(2)

		count := 0
		for range p.Fetch(ctx, mockQuery) {
			count++
		}
		assert.Equal(t, 2, count)
	})
}

func TestPgConnector_CircuitBreaker_Flow(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := NewMockPgxPoolIface(ctrl)
	mockQuery := NewMockPgQuery(ctrl)
	p := setupConnector(t, mockPool)

	dbErr := errors.New("connection reset")
	mockQuery.EXPECT().Name().Return("ErrQuery").AnyTimes()
	mockQuery.EXPECT().SQL().Return("SELECT 1").AnyTimes()

	// ИСПРАВЛЕНИЕ: Вместо nil возвращаем пустую структуру pgconn.CommandTag{}
	mockPool.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(pgconn.CommandTag{}, dbErr).
		Times(3)

	for i := 0; i < 3; i++ {
		_, err := p.Exec(ctx, mockQuery)
		assert.ErrorIs(t, err, dbErr)
	}

	// 2. Цепь разомкнута
	assert.False(t, p.IsOnline())

	// 3. Четвертый вызов не доходит до БД
	_, err := p.Exec(ctx, mockQuery)
	assert.ErrorIs(t, err, gobreaker.ErrOpenState)
}

func TestPgConnector_PanicRecovery(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := NewMockPgxPoolIface(ctrl)
	mockQuery := NewMockPgQuery(ctrl)
	p := setupConnector(t, mockPool)

	mockQuery.EXPECT().Name().Return("PanicQuery").AnyTimes()
	mockQuery.EXPECT().SQL().Return("SELECT 1").AnyTimes()

	// Эмулируем панику внутри Binder (частая ошибка пользователя)
	mockPool.EXPECT().Exec(gomock.Any(), gomock.Any()).Do(func(ctx context.Context, sql string, args ...any) {
		panic("binder error")
	})

	assert.Panics(t, func() {
		_, _ = p.Exec(ctx, mockQuery)
	})
}

func TestPgConnector_String(t *testing.T) {
	p := &pgConnector{host: "localhost", port: 5432, database: "murl"}
	assert.Equal(t, "localhost:5432/murl", p.String())
}

func TestPgConnector_SendBatch(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPool := NewMockPgxPoolIface(ctrl)
	p := setupConnector(t, mockPool)

	type res struct{ ID int }
	q := NewQuery[res]("INSERT INTO t (id) VALUES ($1) RETURNING id", func(r *res) []any {
		return []any{&r.ID}
	})

	t.Run("Success: process multiple queries in batch", func(t *testing.T) {
		argsMatrix := [][]any{{1}, {2}}

		mockBR := NewMockBatchResults(ctrl)

		// 1. Ожидаем отправку батча
		mockPool.EXPECT().
			SendBatch(gomock.Any(), gomock.Any()).
			Return(mockBR)

		// 2. Эмулируем чтение результатов для первого запроса
		mockRows1 := NewMockRows(ctrl)
		mockBR.EXPECT().Query().Return(mockRows1, nil)
		mockRows1.EXPECT().Next().Return(true)
		mockRows1.EXPECT().Scan(gomock.Any()).DoAndReturn(func(dest ...any) error {
			*(dest[0].(*int)) = 1
			return nil
		})
		mockRows1.EXPECT().Next().Return(false)
		mockRows1.EXPECT().Err().Return(nil).AnyTimes()
		mockRows1.EXPECT().Close().AnyTimes()

		// 3. Эмулируем чтение результатов для второго запроса
		mockRows2 := NewMockRows(ctrl)
		mockBR.EXPECT().Query().Return(mockRows2, nil)
		mockRows2.EXPECT().Next().Return(true)
		mockRows2.EXPECT().Scan(gomock.Any()).DoAndReturn(func(dest ...any) error {
			*(dest[0].(*int)) = 2
			return nil
		})
		mockRows2.EXPECT().Next().Return(false)
		mockRows2.EXPECT().Err().Return(nil).AnyTimes()
		mockRows2.EXPECT().Close().AnyTimes()

		// 4. Закрытие BatchResults
		mockBR.EXPECT().Close()

		// Выполняем и собираем результаты
		var results []int
		for val, err := range p.SendBatch(ctx, q, argsMatrix) {
			require.NoError(t, err)
			if val != nil {
				results = append(results, val.(*res).ID)
			}
		}

		assert.Equal(t, []int{1, 2}, results)
	})

	t.Run("Error: fails mid-batch", func(t *testing.T) {
		argsMatrix := [][]any{{1}, {2}}
		mockBR := NewMockBatchResults(ctrl)

		mockPool.EXPECT().SendBatch(gomock.Any(), gomock.Any()).Return(mockBR)

		// Первый запрос
		mockRows := NewMockRows(ctrl)
		mockBR.EXPECT().Query().Return(mockRows, nil)
		mockRows.EXPECT().Next().Return(false)
		mockRows.EXPECT().Err().Return(nil).AnyTimes()
		mockRows.EXPECT().Close().AnyTimes()

		// Второй запрос
		dbErr := errors.New("batch error at index 1")
		mockBR.EXPECT().Query().Return(nil, dbErr)

		mockBR.EXPECT().Close()

		var errs []error
		for _, err := range p.SendBatch(ctx, q, argsMatrix) {
			if err != nil {
				errs = append(errs, err)
			}
		}

		assert.Len(t, errs, 1)
		assert.ErrorIs(t, errs[0], dbErr)
	})
}
