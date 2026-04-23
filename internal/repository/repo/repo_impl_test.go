package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"murl/internal/dto"
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
	inputPayload := dto.AddURL{OriginalURL: originalURL}
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
			p, _ := event.GetPayload[dto.AddURL](e)
			assert.Equal(t, shortPath, p.ShortURL)
			return nil
		})

	// 4. Запускаем обработку
	err := r.On(ctx, inputEvent)

	require.NoError(t, err)
}

func TestRepo_On_EvAddURL_ConflictNoWAL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	mockWAL := NewMockWAL(ctrl)

	r := &repo{repoLinks: mockLinks, wal: mockWAL}

	ctx := context.Background()
	inputPayload := dto.AddURL{OriginalURL: "https://google.com"}
	inputEvent, _ := event.MakeEvent(inputPayload, nil)

	// Имитируем конфликт (URL уже есть)
	mockLinks.EXPECT().
		UpSert(ctx, gomock.Any()).
		Return("/.ABC", true, nil)

	// ВАЖНО: WAL.Push НЕ должен вызываться при конфликте согласно логике repo_impl.go
	mockWAL.EXPECT().Push(gomock.Any()).Times(0)

	err := r.On(ctx, inputEvent)
	require.NoError(t, err)
}

func TestRepo_On_EvGetURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	r := &repo{repoLinks: mockLinks}

	ctx := context.TODO()
	short := "/.AAQ"
	// Метод On предназначен для REPLAY (восстановления состояния).
	// События типа GetURL не изменяют состояние БД, поэтому в методе repo.On
	// для них нет кейса обработки. Тестируем, что On вернет ошибку "not implemented".
	inputEvent, _ := event.MakeEvent(dto.GetURL{ShortURL: short}, nil)

	err := r.On(ctx, inputEvent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestRepo_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWAL := NewMockWAL(ctrl)
	mockCluster := NewMockPgInstance(ctrl)

	r := &repo{
		wal:    mockWAL,
		pgInst: mockCluster,
	}

	mockWAL.EXPECT().Close().Return(nil)
	mockCluster.EXPECT().Close(context.Background()).Return(nil)

	err := r.Close(context.Background())
	assert.NoError(t, err)
}

func TestRepo_Ping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCluster := NewMockPgInstance(ctrl)
	r := &repo{pgInst: mockCluster}

	mockCluster.EXPECT().Ping(gomock.Any()).Return(nil)

	err := r.Ping(context.Background())
	assert.NoError(t, err)
}
