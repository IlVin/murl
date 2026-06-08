package inmem

import (
	"context"
	"testing"

	"murl/internal/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupStatsRepo создает экземпляр репозитория статистики для тестирования
func setupStatsRepo(t *testing.T, size byte) repository.RepoStats {
	core, err := NewInMemCore(testConfig{size: size})
	require.NoError(t, err)
	return NewInMemRepoStats(core)
}

func TestInMemRepoStats_GetStats(t *testing.T) {
	t.Run("Empty storage - returns zero statistics", func(t *testing.T) {
		repo := setupStatsRepo(t, 4)
		ctx := context.Background()

		stats, err := repo.GetStats(ctx)

		assert.NoError(t, err)
		assert.Equal(t, uint64(0), stats.URLs)
		assert.Equal(t, uint64(0), stats.Users)
	})

	t.Run("After adding URLs - returns correct statistics", func(t *testing.T) {
		repo := setupStatsRepo(t, 4)
		ctx := context.Background()

		// Добавляем URL через репозиторий ссылок
		linksRepo := NewInMemRepoLinks(repo.(*InMemRepoStats).c)

		// Добавляем несколько уникальных URL
		_, _, err := linksRepo.UpSert(ctx, "https://example.com/1")
		require.NoError(t, err)

		_, _, err = linksRepo.UpSert(ctx, "https://example.com/2")
		require.NoError(t, err)

		_, _, err = linksRepo.UpSert(ctx, "https://example.com/3")
		require.NoError(t, err)

		stats, err := repo.GetStats(ctx)

		assert.NoError(t, err)
		assert.Equal(t, uint64(3), stats.URLs)
		assert.Equal(t, uint64(0), stats.Users) // Нет сессий
	})

	t.Run("After adding URLs with sessions - returns correct statistics", func(t *testing.T) {
		repo := setupStatsRepo(t, 4)
		ctx := context.Background()

		// Добавляем URL через репозиторий ссылок с сессией
		linksRepo := NewInMemRepoLinks(repo.(*InMemRepoStats).c)
		sessionRepo := NewInMemRepoLinksBySessionID(repo.(*InMemRepoStats).c)

		sessionID1 := "session-1"
		sessionID2 := "session-2"

		// Добавляем ссылки для первой сессии
		_, _, err := sessionRepo.UpSert(ctx, sessionID1, "https://user1.com/1")
		require.NoError(t, err)
		_, _, err = sessionRepo.UpSert(ctx, sessionID1, "https://user1.com/2")
		require.NoError(t, err)

		// Добавляем ссылки для второй сессии
		_, _, err = sessionRepo.UpSert(ctx, sessionID2, "https://user2.com/1")
		require.NoError(t, err)

		// Добавляем анонимную ссылку
		_, _, err = linksRepo.UpSert(ctx, "https://anonymous.com")
		require.NoError(t, err)

		stats, err := repo.GetStats(ctx)

		assert.NoError(t, err)
		assert.Equal(t, uint64(4), stats.URLs)  // 2 + 1 + 1 = 4
		assert.Equal(t, uint64(2), stats.Users) // 2 уникальные сессии
	})

	t.Run("With multiple shards - distributes data correctly", func(t *testing.T) {
		repo := setupStatsRepo(t, 8)
		ctx := context.Background()

		linksRepo := NewInMemRepoLinks(repo.(*InMemRepoStats).c)

		// Добавляем много URL, чтобы они распределились по разным шардам
		for i := 0; i < 100; i++ {
			url := "https://example.com/" + string(rune(i))
			_, _, err := linksRepo.UpSert(ctx, url)
			require.NoError(t, err)
		}

		stats, err := repo.GetStats(ctx)

		assert.NoError(t, err)
		assert.Equal(t, uint64(100), stats.URLs)
		assert.Equal(t, uint64(0), stats.Users)
	})

	t.Run("After deleting data - statistics update correctly", func(t *testing.T) {
		// InMem реализация не поддерживает физическое удаление,
		// только мягкое через флаг deleted (но в статистике мы считаем все URL)
		// Этот тест показывает, что статистика считает все записи
		repo := setupStatsRepo(t, 4)
		ctx := context.Background()

		linksRepo := NewInMemRepoLinks(repo.(*InMemRepoStats).c)

		// Добавляем URL
		url := "https://test-delete.com"
		shortPath, _, err := linksRepo.UpSert(ctx, url)
		require.NoError(t, err)

		// Проверяем статистику до "удаления"
		stats1, err := repo.GetStats(ctx)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), stats1.URLs)

		// InMem не имеет метода Delete, но Select должен работать
		res, _, err := linksRepo.Select(ctx, shortPath)
		assert.NoError(t, err)
		assert.Equal(t, url, res)

		// Статистика должна остаться прежней (нет физического удаления)
		stats2, err := repo.GetStats(ctx)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), stats2.URLs)
	})

	t.Run("Concurrent operations - thread safe", func(t *testing.T) {
		repo := setupStatsRepo(t, 8)
		ctx := context.Background()
		linksRepo := NewInMemRepoLinks(repo.(*InMemRepoStats).c)

		// Параллельно добавляем URL
		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func(id int) {
				url := "https://concurrent.com/" + string(rune(id))
				_, _, err := linksRepo.UpSert(ctx, url)
				assert.NoError(t, err)
				done <- true
			}(i)
		}

		// Ждем завершения всех горутин
		for i := 0; i < 10; i++ {
			<-done
		}

		// Статистика должна быть консистентной
		stats, err := repo.GetStats(ctx)
		assert.NoError(t, err)
		assert.Equal(t, uint64(10), stats.URLs)
	})

	t.Run("Large number of users and URLs", func(t *testing.T) {
		repo := setupStatsRepo(t, 16)
		ctx := context.Background()
		sessionRepo := NewInMemRepoLinksBySessionID(repo.(*InMemRepoStats).c)

		numUsers := 50
		urlsPerUser := 10

		// Добавляем данные
		for userID := 0; userID < numUsers; userID++ {
			sessionID := "user-" + string(rune(userID))
			for urlID := 0; urlID < urlsPerUser; urlID++ {
				url := "https://user" + string(rune(userID)) + ".com/" + string(rune(urlID))
				_, _, err := sessionRepo.UpSert(ctx, sessionID, url)
				require.NoError(t, err)
			}
		}

		stats, err := repo.GetStats(ctx)

		assert.NoError(t, err)
		assert.Equal(t, uint64(numUsers*urlsPerUser), stats.URLs)
		assert.Equal(t, uint64(numUsers), stats.Users)
	})
}

func TestInMemRepoStats_InterfaceCompliance(t *testing.T) {
	var _ repository.RepoStats = (*InMemRepoStats)(nil)
}
