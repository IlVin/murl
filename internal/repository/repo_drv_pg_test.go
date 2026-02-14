package repository

import (
	"context"
	"errors"
	"testing"

	"murl/internal/model/event"

	"github.com/golang/mock/gomock"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
)

// MockRepoConfig для newPgRepoDrv
type mockRepoConfig struct {
	dsn  string
	size uint
}

func (m mockRepoConfig) DBDSN() string   { return m.dsn }
func (m mockRepoConfig) ShardSize() uint { return m.size }

func TestPgRepoDrv_RunMigrations(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m1 := NewMockDBHandler(ctrl)
	m2 := NewMockDBHandler(ctrl)

	// Настраиваем два шарда, указывающих на ОДИН инстанс, и один на другой
	m1.EXPECT().Instance().Return("host1").AnyTimes()
	m2.EXPECT().Instance().Return("host2").AnyTimes()

	r := &PgRepoDrv{shards: []DBHandler{m1, m1, m2}}

	t.Run("Success deduplicated migration", func(t *testing.T) {
		m1.EXPECT().RunMigrations(gomock.Any()).Return(nil)
		m2.EXPECT().RunMigrations(gomock.Any()).Return(nil)

		err := r.RunMigrations(context.Background())
		assert.NoError(t, err)
	})

	t.Run("Context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := r.RunMigrations(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "migration interrupted")
	})
}

func TestPgRepoDrv_UpSert(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m := NewMockDBHandler(ctrl)
	r := &PgRepoDrv{shards: []DBHandler{m}}
	ctx := context.Background()

	t.Run("Success insert", func(t *testing.T) {
		m.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, cb func(context.Context, pgx.Tx) (any, error)) (any, error) {
				return upsertResult{ID: 10, CF: 0}, nil
			})
		id, err := r.UpSert(ctx, 0, "url")
		assert.NoError(t, err)
		assert.Equal(t, uint64(10), id)
	})

	t.Run("Conflict case", func(t *testing.T) {
		m.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(upsertResult{ID: 10, CF: 1}, nil)
		id, err := r.UpSert(ctx, 0, "url")
		assert.ErrorIs(t, err, ErrRecordAlreadyExists)
		assert.Equal(t, uint64(10), id)
	})

	t.Run("Invalid ShardID", func(t *testing.T) {
		_, err := r.UpSert(ctx, 1, "url")
		assert.Error(t, err)
	})
}

func TestPgRepoDrv_BatchUpSert(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m := NewMockDBHandler(ctrl)
	r := &PgRepoDrv{shards: []DBHandler{m}}
	ctx := context.Background()

	t.Run("Success Batch", func(t *testing.T) {
		batch := []event.PayloadBatchItem{
			{ShardID: 0, OrigURL: "u1"},
			{ShardID: 0, OrigURL: "u2"},
		}

		// Мокаем Tx, имитируя успешное выполнение pgx.Batch (логика внутри Tx скрыта интерфейсом)
		m.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, cb func(context.Context, pgx.Tx) (any, error)) (any, error) {
				// В тестах мы не можем легко проэмулировать поведение pgx.Batch без реальной БД,
				// поэтому мы вручную проставляем Idx, как это сделал бы колбек.
				batch[0].Idx = 1
				batch[1].Idx = 2
				return nil, nil
			})

		res, err := r.BatchUpSert(ctx, batch)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), res[0].Idx)
	})

	t.Run("Shard Failure", func(t *testing.T) {
		batch := []event.PayloadBatchItem{{ShardID: 0, OrigURL: "u1"}}
		m.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(nil, errors.New("db fail"))

		res, err := r.BatchUpSert(ctx, batch)
		assert.NoError(t, err) // BatchUpSert не возвращает ошибку, а пишет её в айтем
		assert.Equal(t, errInternalServerError, res[0].Err)
	})
}

func TestPgRepoDrv_Set_Select_Ping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m := NewMockDBHandler(ctrl)
	r := &PgRepoDrv{shards: []DBHandler{m}}
	ctx := context.Background()

	t.Run("Set success", func(t *testing.T) {
		m.EXPECT().PgPool(gomock.Any(), gomock.Any()).Return(nil)
		err := r.Set(ctx, 0, 1, "url")
		assert.NoError(t, err)
	})

	t.Run("Select success", func(t *testing.T) {
		// Для Select Scan(&u) мокаем через вызов замыкания
		m.EXPECT().PgPool(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, cb func(context.Context, *pgxpool.Pool) error) error {
				return nil // В реальном тесте тут была бы логика Scan
			})
		_, err := r.Select(ctx, 0, 1)
		assert.NoError(t, err)
	})

	t.Run("Ping all", func(t *testing.T) {
		m.EXPECT().Ping(gomock.Any()).Return(nil)
		err := r.Ping(ctx)
		assert.NoError(t, err)
	})
}

func TestPgRepoDrv_UnexpectedType(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	m := NewMockDBHandler(ctrl)
	r := &PgRepoDrv{shards: []DBHandler{m}}

	m.EXPECT().Tx(gomock.Any(), gomock.Any()).Return("wrong type", nil)
	_, err := r.UpSert(context.Background(), 0, "url")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected return type")
}
