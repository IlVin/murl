package pg

import (
	"context"
	"errors"
	"testing"

	"murl/internal/dto"
	"murl/internal/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPgRepoStats_GetStats(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInst := NewMockPgInstance(ctrl)
	repo := NewPgRepoStats(mockInst)
	ctx := context.Background()

	t.Run("Success - returns valid statistics", func(t *testing.T) {
		expectedRes := &StatsResult{Users: 25, URLs: 100}

		mockInst.EXPECT().
			FetchRow(ctx, sqlRepoStats).
			Return(expectedRes, nil)

		stats, err := repo.GetStats(ctx)

		require.NoError(t, err)
		assert.Equal(t, uint64(100), stats.URLs)
		assert.Equal(t, uint64(25), stats.Users)
	})

	t.Run("Success - returns zero statistics", func(t *testing.T) {
		expectedRes := &StatsResult{Users: 0, URLs: 0}

		mockInst.EXPECT().
			FetchRow(ctx, sqlRepoStats).
			Return(expectedRes, nil)

		stats, err := repo.GetStats(ctx)

		require.NoError(t, err)
		assert.Equal(t, uint64(0), stats.URLs)
		assert.Equal(t, uint64(0), stats.Users)
	})

	t.Run("Error - database query fails", func(t *testing.T) {
		mockInst.EXPECT().
			FetchRow(ctx, sqlRepoStats).
			Return(nil, errors.New("connection refused"))

		stats, err := repo.GetStats(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute query")
		assert.Equal(t, dto.Stats{}, stats)
	})

	t.Run("Error - invalid result type", func(t *testing.T) {
		// Возвращаем неправильный тип (не *StatsResult)
		mockInst.EXPECT().
			FetchRow(ctx, sqlRepoStats).
			Return("invalid type", nil)

		stats, err := repo.GetStats(ctx)

		assert.Error(t, err)
		assert.Equal(t, dto.Stats{}, stats)
	})

	t.Run("Success - large numbers", func(t *testing.T) {
		expectedRes := &StatsResult{Users: 100500, URLs: 999999}

		mockInst.EXPECT().
			FetchRow(ctx, sqlRepoStats).
			Return(expectedRes, nil)

		stats, err := repo.GetStats(ctx)

		require.NoError(t, err)
		assert.Equal(t, uint64(999999), stats.URLs)
		assert.Equal(t, uint64(100500), stats.Users)
	})
}

func TestPgRepoStats_InterfaceCompliance(t *testing.T) {
	// Проверяем, что PgRepoStats реализует интерфейс repository.RepoStats
	var _ repository.RepoStats = (*PgRepoStats)(nil)
}
