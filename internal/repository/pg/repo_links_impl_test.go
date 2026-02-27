package pg

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"murl/internal/mocks"
	"murl/internal/model/event"
	"murl/internal/repository/pgc"
)

func TestPgRepoLinks_UpSert_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCluster := mocks.NewMockPgCluster(ctrl)
	mockInstance := mocks.NewMockPgInstance(ctrl)
	mockRow := mocks.NewMockRow(ctrl)

	repo := NewPgRepoLinks(mockCluster)
	url := "https://example.com"
	ctx := context.Background()

	mockCluster.EXPECT().ShardID(url).Return(byte(1))
	mockCluster.EXPECT().GetShard(byte(1)).Return(mockInstance, nil)

	mockInstance.EXPECT().Tx(ctx, gomock.Any()).DoAndReturn(
		func(ctx context.Context, cb func(context.Context, pgc.PgxTxIface) error) error {
			mockTx := mocks.NewMockPgxTxIface(ctrl)
			mockTx.EXPECT().QueryRow(ctx, sqlUpSert, url).Return(mockRow)

			// ИСПРАВЛЕНИЕ ЗДЕСЬ: Scan принимает []any
			mockRow.EXPECT().Scan(gomock.Any()).DoAndReturn(func(dest ...any) error {
				// dest[0] это *uint64 (idx)
				// dest[1] это *bool (cf)
				*(dest[0].(*uint64)) = 123
				*(dest[1].(*bool)) = false
				return nil
			})

			return cb(ctx, mockTx)
		})

	short, cf, err := repo.UpSert(ctx, url)

	require.NoError(t, err)
	assert.False(t, cf)
	assert.Contains(t, short, "/.")
}

func TestPgRepoLinks_Select_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCluster := mocks.NewMockPgCluster(ctrl)
	mockInstance := mocks.NewMockPgInstance(ctrl)
	mockRow := mocks.NewMockRow(ctrl)

	repo := NewPgRepoLinks(mockCluster)
	// Валидный shortPath для ShardID=0, Idx=1
	shortPath := "/.AAQ"
	ctx := context.Background()

	mockCluster.EXPECT().Size().Return(byte(2)).AnyTimes()
	mockCluster.EXPECT().GetShard(byte(0)).Return(mockInstance, nil)

	mockInstance.EXPECT().PgPool(ctx, gomock.Any()).DoAndReturn(
		func(ctx context.Context, cb func(context.Context, pgc.PgxPoolIface) error) error {
			mockPool := mocks.NewMockPgxPoolIface(ctrl)
			mockPool.EXPECT().QueryRow(ctx, sqlSelect, uint64(1)).Return(mockRow)
			mockRow.EXPECT().Scan(gomock.Any()).SetArg(0, "https://original.url").Return(nil)

			return cb(ctx, mockPool)
		})

	res, err := repo.Select(ctx, shortPath)

	assert.NoError(t, err)
	assert.Equal(t, "https://original.url", res)
}

func TestPgRepoLinks_BatchUpSert_ErrorHandling(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCluster := mocks.NewMockPgCluster(ctrl)
	repo := NewPgRepoLinks(mockCluster)

	// Тестируем ситуацию, когда GetShard вернул ошибку
	mockCluster.EXPECT().ShardID(gomock.Any()).Return(byte(0)).AnyTimes()
	mockCluster.EXPECT().GetShard(byte(0)).Return(nil, errors.New("shard down"))

	batch := event.PayloadBatch{
		Batch: []event.PayloadBatchItem{
			{OriginalURL: "https://fail.com"},
		},
	}

	result := repo.BatchUpSert(context.Background(), batch)

	require.Equal(t, 1, len(result.Batch))
	assert.NotEmpty(t, result.Batch[0].Err)
	assert.Equal(t, ErrInternalServerError.Error(), result.Batch[0].Err)
}
