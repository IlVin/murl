package repository

import (
	"context"
	"testing"

	"murl/internal/dto"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// TestRepoStatsInterface тестирует интерфейс RepoStats
func TestRepoStatsInterface(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepoStats(ctrl)
	ctx := context.Background()

	t.Run("GetStats returns valid statistics", func(t *testing.T) {
		expected := dto.Stats{
			URLs:  100,
			Users: 25,
		}

		mock.EXPECT().GetStats(ctx).Return(expected, nil).Times(1)

		result, err := mock.GetStats(ctx)

		assert.NoError(t, err)
		assert.Equal(t, expected, result)
		assert.Equal(t, uint64(100), result.URLs)
		assert.Equal(t, uint64(25), result.Users)
	})

	t.Run("GetStats returns empty statistics", func(t *testing.T) {
		expected := dto.Stats{}

		mock.EXPECT().GetStats(ctx).Return(expected, nil).Times(1)

		result, err := mock.GetStats(ctx)

		assert.NoError(t, err)
		assert.Equal(t, uint64(0), result.URLs)
		assert.Equal(t, uint64(0), result.Users)
	})

	t.Run("GetStats returns error", func(t *testing.T) {
		mock.EXPECT().GetStats(ctx).Return(dto.Stats{}, assert.AnError).Times(1)

		result, err := mock.GetStats(ctx)

		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Equal(t, dto.Stats{}, result)
	})

	t.Run("GetStats with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		expectedErr := context.Canceled
		mock.EXPECT().GetStats(ctx).Return(dto.Stats{}, expectedErr).Times(1)

		result, err := mock.GetStats(ctx)

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Equal(t, dto.Stats{}, result)
	})
}

type testRepo struct{}

func (r testRepo) GetStats(ctx context.Context) (dto.Stats, error) {
	return dto.Stats{}, nil
}

// TestRepoStats_InterfaceCompliance проверяет соответствие интерфейсу
func TestRepoStats_InterfaceCompliance(t *testing.T) {
	// Эта проверка гарантирует, что mock реализует интерфейс RepoStats
	var _ RepoStats = (*MockRepoStats)(nil)

	// Проверка, что интерфейс может быть реализован любой структурой

	var _ RepoStats = testRepo{}
}

// TestRepoStats_MultipleCalls тестирует множественные вызовы GetStats
func TestRepoStats_MultipleCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepoStats(ctrl)
	ctx := context.Background()

	// Ожидаем два разных результата
	stats1 := dto.Stats{URLs: 10, Users: 3}
	stats2 := dto.Stats{URLs: 20, Users: 5}

	mock.EXPECT().GetStats(ctx).Return(stats1, nil).Times(1)
	mock.EXPECT().GetStats(ctx).Return(stats2, nil).Times(1)

	result1, err := mock.GetStats(ctx)
	assert.NoError(t, err)
	assert.Equal(t, stats1, result1)

	result2, err := mock.GetStats(ctx)
	assert.NoError(t, err)
	assert.Equal(t, stats2, result2)
}

// TestRepoStats_AnyContext тестирует использование gomock.Any() для контекста
func TestRepoStats_AnyContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepoStats(ctrl)
	expected := dto.Stats{URLs: 50, Users: 10}

	// Используем gomock.Any() для любого контекста
	mock.EXPECT().GetStats(gomock.Any()).Return(expected, nil).Times(3)

	ctx1 := context.Background()
	ctx2, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx3, cancel2 := context.WithTimeout(context.Background(), 100)
	defer cancel2()

	result1, err := mock.GetStats(ctx1)
	assert.NoError(t, err)
	assert.Equal(t, expected, result1)

	result2, err := mock.GetStats(ctx2)
	assert.NoError(t, err)
	assert.Equal(t, expected, result2)

	result3, err := mock.GetStats(ctx3)
	assert.NoError(t, err)
	assert.Equal(t, expected, result3)
}

// TestRepoStats_ConcurrentCalls тестирует конкурентные вызовы (демонстрация, что интерфейс поддерживает)
func TestRepoStats_ConcurrentCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mock := NewMockRepoStats(ctrl)
	ctx := context.Background()

	// Ожидаем 10 вызовов
	mock.EXPECT().GetStats(ctx).Return(dto.Stats{URLs: 100, Users: 20}, nil).Times(10)

	// Запускаем несколько горутин
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, err := mock.GetStats(ctx)
			assert.NoError(t, err)
			done <- true
		}()
	}

	// Ждем завершения всех горутин
	for i := 0; i < 10; i++ {
		<-done
	}
}
