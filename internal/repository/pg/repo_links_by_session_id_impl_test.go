package pg

import (
	"context"
	"errors"
	iter "iter"
	"testing"
	"time"

	"murl/internal/adapters/pgc"
	"murl/internal/dto"
	"murl/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPgRepoLinksBySessionID_UpSert(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInst := NewMockPgInstance(ctrl)
	repo := NewPgRepoLinksBySessionID(mockInst)

	ctx := context.Background()
	sessionID := "test-session"
	url := "https://example.com"
	shardID := model.ShardID(sessionID, ClusterSz)

	t.Run("Success", func(t *testing.T) {
		expectedRes := &UpSertBySessIDResult{Idx: 100, Cf: false}

		mockInst.EXPECT().
			FetchRow(ctx, sqlUpSertBySessionID, url, sessionID).
			Return(expectedRes, nil)

		short, cf, err := repo.UpSert(ctx, sessionID, url)

		require.NoError(t, err)
		assert.False(t, cf)

		// Проверяем формат ссылки (должен содержать шард и закодированный ID)
		expectedShort, _ := model.MakeShortPath(shardID, 100)
		assert.Equal(t, expectedShort, short)
	})

	t.Run("DB Error", func(t *testing.T) {
		mockInst.EXPECT().
			FetchRow(ctx, sqlUpSertBySessionID, url, sessionID).
			Return(nil, errors.New("db fail"))

		_, _, err := repo.UpSert(ctx, sessionID, url)
		assert.Error(t, err)
	})
}

func TestPgRepoLinksBySessionID_SelectAll(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInst := NewMockPgInstance(ctrl)
	repo := NewPgRepoLinksBySessionID(mockInst)
	ctx := context.Background()
	sessionID := "user-1"

	t.Run("Success with multiple items", func(t *testing.T) {
		// Имитируем работу итератора pgc.Fetch
		mockInst.EXPECT().
			Fetch(ctx, sqlSelectAllBySessionID, sessionID).
			Return(func(yield func(any, error) bool) {
				yield(&SelectAllBySessionIDResult{Idx: 1, OriginalURL: "url1"}, nil)
				yield(&SelectAllBySessionIDResult{Idx: 2, OriginalURL: "url2"}, nil)
			})

		items, err := repo.SelectAll(ctx, sessionID)
		require.NoError(t, err)
		assert.Len(t, items, 2)
		assert.Equal(t, "url1", items[0].OriginalURL)
	})

	t.Run("Error during iteration", func(t *testing.T) {
		mockInst.EXPECT().
			Fetch(ctx, sqlSelectAllBySessionID, sessionID).
			Return(func(yield func(any, error) bool) {
				yield(nil, errors.New("scan error"))
			})

		_, err := repo.SelectAll(ctx, sessionID)
		assert.Error(t, err)
	})
}

func TestPgRepoLinksBySessionID_BatchDelBySessionID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInst := NewMockPgInstance(ctrl)
	repo := NewPgRepoLinksBySessionID(mockInst)
	ctx := context.Background()
	sessionID := "deleter"

	t.Run("Enqueue delete items", func(t *testing.T) {
		batch := dto.DeleteURLBySessionID{
			ShortURLs: []string{"/.AAQ"}, // Idx=1
		}

		// Канал для синхронизации: тест не закончится, пока мок не будет вызван
		done := make(chan struct{})

		mockInst.EXPECT().
			SendBatch(gomock.Any(), sqlDelBySessionID, gomock.Any()).
			DoAndReturn(func(ctx context.Context, q pgc.PgQuery, args [][]any) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					yield(nil, nil)
					close(done) // Сигнализируем, что вызов произошел
				}
			}).AnyTimes()

		err := repo.BatchDelBySessionID(ctx, sessionID, batch)
		assert.NoError(t, err)

		// Ждем вызова в фоновой горутине с таймаутом
		select {
		case <-done:
			// Успех
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for SendBatch call in background goroutine")
		}
	})
}
