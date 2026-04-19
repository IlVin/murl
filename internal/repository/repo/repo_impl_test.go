package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"murl/internal/model/event"
)

func TestRepo_On_EvAddURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	mockWAL := NewMockWAL(ctrl)

	r := &repo{
		repoLinks: mockLinks,
		wal:       mockWAL,
	}

	ctx := context.Background()
	originalURL := "https://yandex.ru"

	// 1. Создаем входное событие
	inputPayload := event.PayloadAddURL{OriginalURL: originalURL}
	inputEvent, _ := event.MakeEvent(inputPayload, nil)

	// 2. Ожидаем вызов в БД (UpSert)
	shortPath := "/.AAQ"
	mockLinks.EXPECT().
		UpSert(ctx, originalURL).
		Return(shortPath, false, nil)

	// 3. Ожидаем запись в WAL (так как conflictFlag = false)
	// Мы проверяем, что в WAL уходит событие с заполненным ShortURL
	mockWAL.EXPECT().
		Push(gomock.Any()).
		DoAndReturn(func(e event.Event) error {
			p, _ := event.GetPayload[event.PayloadAddURL](e)
			assert.Equal(t, shortPath, p.ShortURL)
			return nil
		})

	// 4. Запускаем обработку
	resEvent, err := r.On(ctx, inputEvent)

	require.NoError(t, err)
	resPayload, _ := event.GetPayload[event.PayloadAddURL](resEvent)
	assert.Equal(t, shortPath, resPayload.ShortURL)
}

func TestRepo_On_EvAddURL_ConflictNoWAL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	mockWAL := NewMockWAL(ctrl)

	r := &repo{repoLinks: mockLinks, wal: mockWAL}

	ctx := context.Background()
	inputPayload := event.PayloadAddURL{OriginalURL: "https://google.com"}
	inputEvent, _ := event.MakeEvent(inputPayload, nil)

	// Имитируем конфликт (URL уже есть)
	mockLinks.EXPECT().
		UpSert(ctx, gomock.Any()).
		Return("/.ABC", true, nil)

	// ВАЖНО: WAL.Push НЕ должен вызываться при конфликте согласно логике repo_impl.go
	mockWAL.EXPECT().Push(gomock.Any()).Times(0)

	resEvent, err := r.On(ctx, inputEvent)
	require.NoError(t, err)

	p, _ := event.GetPayload[event.PayloadAddURL](resEvent)
	assert.True(t, p.ConflictFlag)
}

func TestRepo_On_EvGetURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	r := &repo{repoLinks: mockLinks}

	ctx := context.Background()
	short := "/.AAQ"
	original := "https://murl.io"

	inputPayload := event.PayloadGetURL{ShortURL: short}
	inputEvent, _ := event.MakeEvent(inputPayload, nil)

	mockLinks.EXPECT().
		Select(ctx, short).
		Return(original, false, nil)

	resEvent, err := r.On(ctx, inputEvent)
	require.NoError(t, err)

	p, _ := event.GetPayload[event.PayloadGetURL](resEvent)
	assert.Equal(t, original, p.OriginalURL)
}

func TestRepo_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWAL := NewMockWAL(ctrl)
	mockCluster := NewMockPgCluster(ctrl)

	r := &repo{
		wal:       mockWAL,
		pgCluster: mockCluster,
	}

	mockWAL.EXPECT().Close().Return(nil)
	mockCluster.EXPECT().Close().Return(nil)

	err := r.Close()
	assert.NoError(t, err)
}
