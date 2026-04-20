package pgc

import (
	"context"
	"errors"
	"net"
	"testing"

	pgconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	traceNoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"
)

// Специальный тип для имитации сетевой ошибки, которую поймет isRetryable
type myNetError struct{ error }

func (e myNetError) Timeout() bool   { return true }
func (e myNetError) Temporary() bool { return true }

func TestCQRSConnector_Scenarios(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Настройка Noop телеметрии
	tp := traceNoop.NewTracerProvider()

	type res struct{ ID int }
	qRead := NewQuery[res]("SELECT 1", nil).AsRead()
	qWrite := NewQuery[res]("INSERT...", nil).AsWrite()

	t.Run("Empty Replicas: fallback to master", func(t *testing.T) {
		master := NewMockPgInstance(ctrl)
		master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
		master.EXPECT().WithMeterProvider(gomock.Any()).Return(master).AnyTimes()

		db := NewCQRSConnector(master).WithTracerProvider(tp)

		master.EXPECT().FetchRow(gomock.Any(), qRead).Return(&res{ID: 1}, nil)

		val, err := db.FetchRow(ctx, qRead)
		assert.NoError(t, err)
		assert.Equal(t, &res{ID: 1}, val)
	})

	t.Run("Balancing: 10 calls distributed between 2 replicas", func(t *testing.T) {
		master := NewMockPgInstance(ctrl)
		r1 := NewMockPgInstance(ctrl)
		r2 := NewMockPgInstance(ctrl)

		master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
		r1.EXPECT().WithTracerProvider(gomock.Any()).Return(r1).AnyTimes()
		r2.EXPECT().WithTracerProvider(gomock.Any()).Return(r2).AnyTimes()

		db := NewCQRSConnector(master, r1, r2).WithTracerProvider(tp)

		r1.EXPECT().IsOnline().Return(true).AnyTimes()
		r2.EXPECT().IsOnline().Return(true).AnyTimes()

		r1.EXPECT().FetchRow(gomock.Any(), qRead).Return(&res{ID: 1}, nil).Times(5)
		r2.EXPECT().FetchRow(gomock.Any(), qRead).Return(&res{ID: 2}, nil).Times(5)

		for i := 0; i < 10; i++ {
			_, _ = db.FetchRow(ctx, qRead)
		}
	})

	t.Run("Rotation: Subsequent requests should hit different replicas", func(t *testing.T) {
		master := NewMockPgInstance(ctrl)
		r1 := NewMockPgInstance(ctrl)
		r2 := NewMockPgInstance(ctrl)

		master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
		r1.EXPECT().WithTracerProvider(gomock.Any()).Return(r1).AnyTimes()
		r2.EXPECT().WithTracerProvider(gomock.Any()).Return(r2).AnyTimes()

		db := NewCQRSConnector(master, r1, r2).WithTracerProvider(tp)

		r1.EXPECT().IsOnline().Return(true).AnyTimes()
		r2.EXPECT().IsOnline().Return(true).AnyTimes()

		r1.EXPECT().FetchRow(gomock.Any(), qRead).Return(&res{ID: 101}, nil).Times(1)
		r2.EXPECT().FetchRow(gomock.Any(), qRead).Return(&res{ID: 102}, nil).Times(1)

		res1, _ := FetchRow(ctx, db, qRead)
		res2, _ := FetchRow(ctx, db, qRead)

		assert.NotEqual(t, res1.ID, res2.ID, "Запросы должны были попасть на разные реплики")
	})

	t.Run("Failover: skip offline replica", func(t *testing.T) {
		master := NewMockPgInstance(ctrl)
		r1 := NewMockPgInstance(ctrl)
		master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
		r1.EXPECT().WithTracerProvider(gomock.Any()).Return(r1).AnyTimes()

		db := NewCQRSConnector(master, r1).WithTracerProvider(tp)

		r1.EXPECT().IsOnline().Return(false)
		master.EXPECT().FetchRow(gomock.Any(), qRead).Return(&res{ID: 10}, nil)

		resVal, err := db.FetchRow(ctx, qRead)
		assert.NoError(t, err)
		assert.Equal(t, &res{ID: 10}, resVal)
	})

	t.Run("Exec (RW): Always master, retry only on master", func(t *testing.T) {
		master := NewMockPgInstance(ctrl)
		r1 := NewMockPgInstance(ctrl)
		master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
		r1.EXPECT().WithTracerProvider(gomock.Any()).Return(r1).AnyTimes()

		db := NewCQRSConnector(master, r1).WithTracerProvider(tp)

		retryableErr := &pgconn.PgError{Code: "57P01"}
		r1.EXPECT().IsOnline().Times(0)
		master.EXPECT().Exec(gomock.Any(), qWrite).Return(int64(0), retryableErr).Times(2)

		affected, err := Exec(ctx, db, qWrite)
		assert.ErrorIs(t, err, retryableErr)
		assert.Equal(t, int64(0), affected)
	})
}

func TestCQRSConnector_RetryLogic(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)
	r1 := NewMockPgInstance(ctrl)

	tp := traceNoop.NewTracerProvider()
	master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
	r1.EXPECT().WithTracerProvider(gomock.Any()).Return(r1).AnyTimes()

	db := NewCQRSConnector(master, r1).WithTracerProvider(tp)
	q := NewQuery[struct{}]("SELECT 1", nil).AsRead()

	t.Run("Retry: transient error goes to next node", func(t *testing.T) {
		var errRetry net.Error = myNetError{errors.New("network fail")}

		r1.EXPECT().IsOnline().Return(true).AnyTimes()
		master.EXPECT().IsOnline().Return(true).AnyTimes()

		r1.EXPECT().FetchRow(gomock.Any(), q).Return(nil, errRetry).AnyTimes()
		master.EXPECT().FetchRow(gomock.Any(), q).Return("success", nil).AnyTimes()

		res, err := db.FetchRow(ctx, q)

		assert.NoError(t, err)
		assert.Equal(t, "success", res)
	})
}

func TestCQRSConnector_SendBatch_Scenarios(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)
	replica := NewMockPgInstance(ctrl)
	tp := traceNoop.NewTracerProvider()

	master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
	replica.EXPECT().WithTracerProvider(gomock.Any()).Return(replica).AnyTimes()

	db := NewCQRSConnector(master, replica).WithTracerProvider(tp)

	type res struct{ ID int }
	qRead := NewQuery[res]("SELECT id", nil).AsRead()
	qWrite := NewQuery[res]("INSERT...", nil).AsWrite()
	args := [][]any{{1}, {2}}

	t.Run("RO Batch: should go to replica", func(t *testing.T) {
		replica.EXPECT().IsOnline().Return(true).AnyTimes()

		replica.EXPECT().SendBatch(gomock.Any(), qRead, args).Return(func(yield func(any, error) bool) {
			yield(&res{ID: 1}, nil)
		})
		master.EXPECT().SendBatch(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		count := 0
		for range db.SendBatch(ctx, qRead, args) {
			count++
		}
		assert.Equal(t, 1, count)
	})

	t.Run("RW Batch Retry: transparent for user", func(t *testing.T) {
		netErr := &net.OpError{Op: "write", Net: "tcp", Err: errors.New("reset")}

		master.EXPECT().SendBatch(gomock.Any(), qWrite, args).Return(func(yield func(any, error) bool) {
			yield(nil, netErr)
		}).Times(1)

		master.EXPECT().SendBatch(gomock.Any(), qWrite, args).Return(func(yield func(any, error) bool) {
			yield(&res{ID: 100}, nil)
		}).Times(1)

		count := 0
		for range db.SendBatch(ctx, qWrite, args) {
			count++
		}

		assert.Equal(t, 1, count)
	})
}

func TestCQRSConnector_Fetch_And_Rotation(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)
	r1 := NewMockPgInstance(ctrl)
	r2 := NewMockPgInstance(ctrl)

	// Настраиваем моки на Fluent API (проброс настроек телеметрии)
	tp := traceNoop.NewTracerProvider()
	master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
	r1.EXPECT().WithTracerProvider(gomock.Any()).Return(r1).AnyTimes()
	r2.EXPECT().WithTracerProvider(gomock.Any()).Return(r2).AnyTimes()

	// Инициализируем коннектор и прокидываем Noop трейсер
	db := NewCQRSConnector(master, r1, r2).WithTracerProvider(tp)

	type res struct{ ID int }
	qRead := NewQuery[res]("SELECT id", nil).AsRead()
	qWrite := NewQuery[res]("UPDATE", nil).AsWrite()

	t.Run("Fetch: Should use first available replica and NOT fallback to master if success", func(t *testing.T) {
		r1.EXPECT().IsOnline().Return(true).AnyTimes()
		r2.EXPECT().IsOnline().Return(true).AnyTimes()

		// Эмулируем успешный итератор от реплики
		mockSeq := func(yield func(any, error) bool) {
			yield(&res{ID: 1}, nil)
		}

		// Ожидаем, что вызов приземлится в ОДНУ из реплик (неважно какую, зависит от индекса)
		// Но мастер НЕ должен вызываться
		master.EXPECT().Fetch(gomock.Any(), qRead).Times(0)

		// Настраиваем так, чтобы одна из реплик ответила
		r1.EXPECT().Fetch(gomock.Any(), qRead).Return(mockSeq).MaxTimes(1)
		r2.EXPECT().Fetch(gomock.Any(), qRead).Return(mockSeq).MaxTimes(1)

		// Выполняем Fetch
		count := 0
		for item, err := range Fetch(ctx, db, qRead) {
			assert.NoError(t, err)
			assert.Equal(t, 1, item.ID)
			count++
		}
		assert.Equal(t, 1, count)
	})

	t.Run("Rotation: Subsequent requests should hit different replicas", func(t *testing.T) {
		r1.EXPECT().IsOnline().Return(true).AnyTimes()
		r2.EXPECT().IsOnline().Return(true).AnyTimes()

		// Первый запрос уходит в r1, второй в r2 (или наоборот, главное - разные)
		r1.EXPECT().FetchRow(gomock.Any(), qRead).Return(&res{ID: 101}, nil).Times(1)
		r2.EXPECT().FetchRow(gomock.Any(), qRead).Return(&res{ID: 102}, nil).Times(1)

		res1, _ := FetchRow(ctx, db, qRead)
		res2, _ := FetchRow(ctx, db, qRead)

		assert.NotEqual(t, res1.ID, res2.ID, "Запросы должны были попасть на разные реплики")
	})

	t.Run("Exec (RW): Always master, retry only on master even if broken", func(t *testing.T) {
		// Ошибка, которую можно ретраить
		retryableErr := &pgconn.PgError{Code: "57P01"}

		// Реплики онлайн, но они НЕ ДОЛЖНЫ даже опрашиваться для Exec
		r1.EXPECT().IsOnline().Times(0)
		r2.EXPECT().IsOnline().Times(0)

		// Ожидаем ровно 2 попытки в МАСТЕР и никуда больше
		master.EXPECT().Exec(gomock.Any(), qWrite).Return(int64(0), retryableErr).Times(2)

		affected, err := Exec(ctx, db, qWrite)

		assert.ErrorIs(t, err, retryableErr)
		assert.Equal(t, int64(0), affected)
	})
}

func TestCQRSConnector_PickLiveReplica(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)
	r1 := NewMockPgInstance(ctrl)
	r2 := NewMockPgInstance(ctrl)

	// Настройка Noop телеметрии через Fluent API
	tp := traceNoop.NewTracerProvider()
	master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
	r1.EXPECT().WithTracerProvider(gomock.Any()).Return(r1).AnyTimes()
	r2.EXPECT().WithTracerProvider(gomock.Any()).Return(r2).AnyTimes()

	// Используем конкретный тип для теста
	type testRes struct{ Val string }
	q := NewQuery[testRes]("SELECT 1", nil).AsRead()

	// Создаем коннектор и прокидываем трейсер
	db := NewCQRSConnector(master, r1, r2).WithTracerProvider(tp)

	t.Run("Should skip r1 (offline) and use r2 (online) instead of master", func(t *testing.T) {
		r1.EXPECT().IsOnline().Return(false).AnyTimes()
		r2.EXPECT().IsOnline().Return(true).AnyTimes()

		// Мастер и r1 не должны вызываться для выполнения запроса
		master.EXPECT().FetchRow(gomock.Any(), q, gomock.Any()).Times(0)
		r1.EXPECT().FetchRow(gomock.Any(), q, gomock.Any()).Times(0)

		// Мок должен вернуть указатель на структуру
		successValue := &testRes{Val: "success_from_r2"}
		r2.EXPECT().FetchRow(gomock.Any(), q, gomock.Any()).Return(successValue, nil).Times(1)

		// Используем типизированный FetchRow из pgc.go
		res, err := FetchRow(ctx, db, q, 1)

		assert.NoError(t, err)
		assert.Equal(t, "success_from_r2", res.Val)
	})
}
