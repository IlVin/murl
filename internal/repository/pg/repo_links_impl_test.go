package pg

import (
	"context"
	"errors"
	"murl/internal/dto"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPgRepoLinks_UpSert_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInstance := NewMockPgInstance(ctrl)

	repo := NewPgRepoLinks(mockInstance)
	url := "https://example.com"
	ctx := context.Background()

	mockInstance.EXPECT().
		FetchRow(ctx, sqlUpSert, url).
		Return(&UpSertResult{Idx: 123, Cf: false}, nil)

	short, cf, err := repo.UpSert(ctx, url)

	require.NoError(t, err)
	assert.False(t, cf)
	assert.Contains(t, short, "/.")
}

func TestPgRepoLinks_Select_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInstance := NewMockPgInstance(ctrl)

	repo := NewPgRepoLinks(mockInstance)
	// Валидный shortPath для ShardID=0, Idx=1
	shortPath := "/.AAQ"
	ctx := context.Background()

	mockInstance.EXPECT().
		FetchRow(ctx, sqlSelect, uint64(1)).
		Return(&SelectResult{URL: "https://original.url", Deleted: false}, nil)

	res, _, err := repo.Select(ctx, shortPath)

	assert.NoError(t, err)
	assert.Equal(t, "https://original.url", res)
}

func TestPgRepoLinks_BatchUpSert_ErrorHandling(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInstance := NewMockPgInstance(ctrl)
	repo := NewPgRepoLinks(mockInstance)

	// Имитируем ошибку БД через итератор SendBatch
	mockInstance.EXPECT().
		SendBatch(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(func(yield func(any, error) bool) {
			yield(nil, errors.New("db connection lost"))
		})

	batch := dto.Batch{
		Batch: []dto.BatchItem{
			{OriginalURL: "https://fail.com"},
		},
	}

	result := repo.BatchUpSert(context.Background(), batch)

	require.Equal(t, 1, len(result.Batch))
	assert.NotEmpty(t, result.Batch[0].Err)
	assert.Equal(t, ErrInternalServerError.Error(), result.Batch[0].Err)
}
