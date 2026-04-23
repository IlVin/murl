package inmem

import (
	"context"
	"math"
	"murl/internal/dto"
	"murl/internal/model"
	"murl/internal/repository"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConfig для реализации InMemConfig внутри тестов
type testConfig struct{ size byte }

func (c testConfig) ShardSize() byte { return c.size }

// setupRepo теперь возвращает интерфейс, как и в реальном приложении
func setupRepo(t *testing.T, size byte) repository.RepoLinks {
	core, err := NewInMemCore(testConfig{size: size})
	require.NoError(t, err)
	return NewInMemRepoLinks(core)
}

func TestUpSert(t *testing.T) {
	repo := setupRepo(t, 2)
	ctx := context.Background()
	url := "https://google.com"

	t.Run("New record", func(t *testing.T) {
		path, conflict, err := repo.UpSert(ctx, url)
		assert.NoError(t, err)
		assert.False(t, conflict)
		assert.NotEmpty(t, path)
	})

	t.Run("Conflict under RLock", func(t *testing.T) {
		path, conflict, err := repo.UpSert(ctx, url)
		assert.NoError(t, err)
		assert.True(t, conflict)
		assert.NotEmpty(t, path)
	})

	t.Run("Overflow error", func(t *testing.T) {
		// Кастим интерфейс к реализации для доступа к внутреннему состоянию (White-box testing)
		impl := repo.(*InMemRepoLinks)

		shardID := impl.c.ShardID("overflow-url")
		shard, err := impl.c.GetShard(shardID)
		require.NoError(t, err)

		shard.Mu.Lock()
		shard.LastIdx = math.MaxUint64
		shard.Mu.Unlock()

		_, _, err = repo.UpSert(ctx, "overflow-url")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "overflow")
	})
}

func TestSelect(t *testing.T) {
	repo := setupRepo(t, 1)
	ctx := context.Background()
	url := "https://test.com"
	path, _, _ := repo.UpSert(ctx, url)

	t.Run("Success", func(t *testing.T) {
		res, _, err := repo.Select(ctx, path)
		assert.NoError(t, err)
		assert.Equal(t, url, res)
	})

	t.Run("Invalid path format", func(t *testing.T) {
		_, _, err := repo.Select(ctx, "invalid-marker-less-path")
		assert.Error(t, err)
	})

	t.Run("Record not found", func(t *testing.T) {
		// Генерируем валидный путь для индекса, которого нет (999)
		fakePath, _, _ := makeShortPath(0, 999, false)
		_, _, err := repo.Select(ctx, fakePath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestSet(t *testing.T) {
	repo := setupRepo(t, 2)
	ctx := context.Background()
	url := "https://original.com"
	path, err := model.MakeShortPath(1, 13)
	assert.NoError(t, err)

	t.Run("Hard set and overwrite index", func(t *testing.T) {
		err := repo.Set(ctx, url, path)
		assert.NoError(t, err)

		// Проверяем, что перезапись того же индекса другим URL работает
		newURL := "https://replaced.com"
		err = repo.Set(ctx, newURL, path)
		assert.NoError(t, err)

		res, _, _ := repo.Select(ctx, path)
		assert.Equal(t, newURL, res)
	})

	t.Run("Set invalid data", func(t *testing.T) {
		err := repo.Set(ctx, url, "bad-format")
		assert.Error(t, err)

		// Попытка установить MaxUint64 (запрещено логикой Set)
		err = repo.Set(ctx, url, "/.A//_")
		assert.Error(t, err)
	})
}

func TestBatchUpSert(t *testing.T) {
	repo := setupRepo(t, 4)

	t.Run("Batch Success", func(t *testing.T) {
		batch := dto.Batch{
			Batch: []dto.BatchItem{
				{OriginalURL: "url1"},
				{OriginalURL: "url2"},
			},
		}
		res := repo.BatchUpSert(context.Background(), batch)
		assert.Len(t, res.Batch, 2)
		assert.NotEmpty(t, res.Batch[0].ShortURL)
		assert.NotEmpty(t, res.Batch[1].ShortURL)
	})
}

func TestMakeShortPathInternal(t *testing.T) {
	t.Run("Model Error Forwarding", func(t *testing.T) {
		// shardID 255 гарантированно за пределами словаря base64u
		p, cf, err := makeShortPath(255, 1, true)
		assert.Error(t, err)
		assert.Empty(t, p)
		assert.True(t, cf)
	})
}
