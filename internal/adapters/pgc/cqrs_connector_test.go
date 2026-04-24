package pgc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

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

// TestCQRSConnector_NoRetryAfterFirstRow тестирует критическое правило:
// Если хотя бы одна строка уже передана через yield, ретрай НЕ ДОЛЖЕН выполняться,
// даже если ошибка повторимая.
func TestCQRSConnector_NoRetryAfterFirstRow(t *testing.T) {
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
	q := NewQuery[res]("SELECT id FROM users", func(r *res) []any {
		return []any{&r.ID}
	}).AsRead()

	// Сценарий: реплика отдала первую строку, а затем сломалась
	replicaSeq := func(yield func(any, error) bool) {
		// Отдаём первую строку успешно
		if !yield(&res{ID: 1}, nil) {
			return
		}
		// Затем отдаём ошибку сети (повторимую)
		netErr := mockNetError{error: errors.New("connection lost after first row")}
		yield(nil, netErr)
	}

	replica.EXPECT().IsOnline().Return(true).AnyTimes()
	replica.EXPECT().Fetch(gomock.Any(), q, gomock.Any()).Return(replicaSeq)

	// Мастер НЕ ДОЛЖЕН вызываться, потому что ретрай после первой строки запрещён
	master.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	var results []int
	var lastErr error

	for val, err := range db.Fetch(ctx, q) {
		if err != nil {
			lastErr = err
			break
		}
		if val != nil {
			results = append(results, val.(*res).ID)
		}
	}

	// Должна быть получена первая строка
	assert.Equal(t, []int{1}, results, "Должна быть получена первая строка")
	// Должна быть ошибка (реплика сломалась после первой строки)
	assert.Error(t, lastErr, "Должна быть ошибка после первой строки")
	assert.Contains(t, lastErr.Error(), "connection lost", "Ошибка должна быть исходной сетевой")
}

// TestCQRSConnector_RetryOnlyBeforeFirstRow тестирует, что ретрай происходит ТОЛЬКО
// если ошибка случилась ДО передачи первой строки.
func TestCQRSConnector_RetryOnlyBeforeFirstRow(t *testing.T) {
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
	q := NewQuery[res]("SELECT id FROM users", func(r *res) []any {
		return []any{&r.ID}
	}).AsRead()

	// Сценарий: реплика сразу возвращает сетевую ошибку (повторимую)
	replicaSeq := func(yield func(any, error) bool) {
		netErr := mockNetError{error: errors.New("network timeout")}
		yield(nil, netErr)
	}

	// Мастер успешно возвращает данные
	masterSeq := func(yield func(any, error) bool) {
		yield(&res{ID: 42}, nil)
	}

	replica.EXPECT().IsOnline().Return(true).AnyTimes()
	replica.EXPECT().Fetch(gomock.Any(), q, gomock.Any()).Return(replicaSeq)
	master.EXPECT().IsOnline().Return(true).AnyTimes()
	master.EXPECT().Fetch(gomock.Any(), q, gomock.Any()).Return(masterSeq)

	var results []int
	for val, err := range db.Fetch(ctx, q) {
		if err != nil {
			t.Fatalf("Не ожидалась ошибка, получена: %v", err)
		}
		if val != nil {
			results = append(results, val.(*res).ID)
		}
	}

	// Должны получить данные от мастера
	assert.Equal(t, []int{42}, results, "Ретрай должен переключиться на мастера")
}

// TestCQRSConnector_FetchRowRetryStrategy тестирует стратегию ретраев для FetchRow
// (здесь ретрай всегда безопасен, так как нет частичных результатов).
func TestCQRSConnector_FetchRowRetryStrategy(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)
	replica1 := NewMockPgInstance(ctrl)
	replica2 := NewMockPgInstance(ctrl)

	tp := traceNoop.NewTracerProvider()
	master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
	replica1.EXPECT().WithTracerProvider(gomock.Any()).Return(replica1).AnyTimes()
	replica2.EXPECT().WithTracerProvider(gomock.Any()).Return(replica2).AnyTimes()

	db := NewCQRSConnector(master, replica1, replica2).WithTracerProvider(tp)

	type res struct{ ID int }
	q := NewQuery[res]("SELECT id FROM users", func(r *res) []any {
		return []any{&r.ID}
	}).AsRead()

	t.Run("retry through all replicas then master", func(t *testing.T) {
		netErr := mockNetError{error: errors.New("connection refused")}

		replica1.EXPECT().IsOnline().Return(true).AnyTimes()
		replica1.EXPECT().FetchRow(gomock.Any(), q, gomock.Any()).Return(nil, netErr)

		replica2.EXPECT().IsOnline().Return(true).AnyTimes()
		replica2.EXPECT().FetchRow(gomock.Any(), q, gomock.Any()).Return(nil, netErr)

		master.EXPECT().IsOnline().Return(true).AnyTimes()
		master.EXPECT().FetchRow(gomock.Any(), q, gomock.Any()).Return(&res{ID: 99}, nil)

		res, err := FetchRow(ctx, db, q)
		assert.NoError(t, err)
		assert.Equal(t, 99, res.ID)
	})

	t.Run("non-retryable error stops immediately", func(t *testing.T) {
		syntaxErr := &pgconn.PgError{Code: "42601", Message: "syntax error"}

		replica1.EXPECT().IsOnline().Return(true).AnyTimes()
		replica1.EXPECT().FetchRow(gomock.Any(), q, gomock.Any()).Return(nil, syntaxErr)

		// Реплика 2 и мастер НЕ ДОЛЖНЫ вызываться
		replica2.EXPECT().FetchRow(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		master.EXPECT().FetchRow(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		_, err := FetchRow(ctx, db, q)
		assert.Error(t, err)
		var pgErr *pgconn.PgError
		assert.ErrorAs(t, err, &pgErr)
		assert.Equal(t, "42601", pgErr.Code)
	})
}

// TestCQRSConnector_ContextCancellationDuringBackoff тестирует, что бэкофф
// правильно реагирует на отмену контекста.
func TestCQRSConnector_ContextCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)

	tp := traceNoop.NewTracerProvider()
	master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()

	db := NewCQRSConnector(master).WithTracerProvider(tp)

	type res struct{ ID int }
	q := NewQuery[res]("SELECT id FROM users", func(r *res) []any {
		return []any{&r.ID}
	}).AsWrite() // Write-запрос, чтобы включился бэкофф

	retryableErr := &pgconn.PgError{Code: "57P01"} // Admin shutdown

	// Первая попытка — ошибка
	master.EXPECT().Exec(gomock.Any(), q, gomock.Any()).Return(int64(0), retryableErr)

	// Отменяем контекст во время бэкоффа
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	_, err := db.Exec(ctx, q)
	elapsed := time.Since(start)

	assert.ErrorIs(t, err, context.Canceled)
	// Бэкофф НЕ должен отработать полностью (100ms)
	assert.Less(t, elapsed, 100*time.Millisecond, "Бэкофф должен прерваться по отмене контекста")
}

// TestCQRSConnector_WriteOnlyMasterNoReplicas тестирует, что write-операции
// никогда не пытаются обратиться к репликам, даже если реплики есть.
func TestCQRSConnector_WriteOnlyMasterNoReplicas(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)
	replica := NewMockPgInstance(ctrl)

	tp := traceNoop.NewTracerProvider()
	master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
	replica.EXPECT().WithTracerProvider(gomock.Any()).Return(replica).AnyTimes()

	db := NewCQRSConnector(master, replica).WithTracerProvider(tp)

	q := NewCommand("UPDATE users SET active = true").AsWrite()

	// Реплика не должна вызываться даже для IsOnline
	replica.EXPECT().IsOnline().Times(0)
	replica.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	// Мастер вызывается (может быть с ретраем)
	master.EXPECT().IsOnline().AnyTimes()
	master.EXPECT().Exec(gomock.Any(), q, gomock.Any()).Return(int64(5), nil).Times(1)

	affected, err := Exec(ctx, db, q)
	assert.NoError(t, err)
	assert.Equal(t, int64(5), affected)
}

// TestCQRSConnector_GetNodeOrder проверяет правильность порядка узлов для разных сценариев.
func TestCQRSConnector_GetNodeOrder(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)
	replica1 := NewMockPgInstance(ctrl)
	replica2 := NewMockPgInstance(ctrl)
	replica3 := NewMockPgInstance(ctrl)

	// Настройка IsOnline: r1 и r3 онлайн, r2 оффлайн
	replica1.EXPECT().IsOnline().Return(true).AnyTimes()
	replica2.EXPECT().IsOnline().Return(false).AnyTimes()
	replica3.EXPECT().IsOnline().Return(true).AnyTimes()

	conn := &cqrsConnector{
		master:   master,
		replicas: []PgInstance{replica1, replica2, replica3},
	}

	t.Run("Write operation: only master twice", func(t *testing.T) {
		nodes := conn.getNodeOrder(false)
		assert.Len(t, nodes, 2)
		assert.Equal(t, master, nodes[0])
		assert.Equal(t, master, nodes[1])
	})

	t.Run("Read operation: online replicas + master", func(t *testing.T) {
		nodes := conn.getNodeOrder(true)
		// Должны быть только живые реплики (r1, r3) + мастер
		assert.Len(t, nodes, 3)
		assert.Equal(t, replica1, nodes[0])
		assert.Equal(t, replica3, nodes[1])
		assert.Equal(t, master, nodes[2])
	})
}

// BenchmarkCQRSConnector_Fetch измеряет производительность Fetch с ретраями.
func BenchmarkCQRSConnector_Fetch(b *testing.B) {
	ctx := context.Background()
	ctrl := gomock.NewController(b)
	defer ctrl.Finish()

	master := NewMockPgInstance(ctrl)
	replica := NewMockPgInstance(ctrl)

	tp := traceNoop.NewTracerProvider()
	master.EXPECT().WithTracerProvider(gomock.Any()).Return(master).AnyTimes()
	replica.EXPECT().WithTracerProvider(gomock.Any()).Return(replica).AnyTimes()

	db := NewCQRSConnector(master, replica).WithTracerProvider(tp)

	type res struct{ ID int }
	q := NewQuery[res]("SELECT id FROM users", func(r *res) []any {
		return []any{&r.ID}
	}).AsRead()

	// Эмулируем успешный итератор
	mockSeq := func(yield func(any, error) bool) {
		for i := 0; i < 100; i++ {
			if !yield(&res{ID: i}, nil) {
				return
			}
		}
	}

	replica.EXPECT().IsOnline().Return(true).AnyTimes()
	replica.EXPECT().Fetch(gomock.Any(), q, gomock.Any()).Return(mockSeq).AnyTimes()
	master.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for range db.Fetch(ctx, q) {
			// Просто потребляем
		}
	}
}
