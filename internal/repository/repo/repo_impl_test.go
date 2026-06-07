package repo

import (
	"context"
	"errors"
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

func TestRepo_AddURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	r := &repo{repoLinks: mockLinks}
	ctx := context.Background()
	originalURL := "https://example.com"
	shortPath := "/.ABC"

	mockLinks.EXPECT().
		UpSert(ctx, originalURL).
		Return(shortPath, false, nil)

	result, err := r.AddURL(ctx, dto.AddURL{OriginalURL: originalURL})

	require.NoError(t, err)
	assert.Equal(t, shortPath, result.ShortURL)
	assert.False(t, result.ConflictFlag)
}

func TestRepo_AddURL_WithSetMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	r := &repo{repoLinks: mockLinks}
	ctx := context.Background()
	originalURL := "https://example.com"
	shortPath := "/.ABC"

	mockLinks.EXPECT().
		Set(ctx, originalURL, shortPath).
		Return(nil)

	result, err := r.AddURL(ctx, dto.AddURL{OriginalURL: originalURL, ShortURL: shortPath})

	require.NoError(t, err)
	assert.Equal(t, shortPath, result.ShortURL)
}

func TestRepo_AddURL_WithWAL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	mockWAL := NewMockWAL(ctrl)
	r := &repo{repoLinks: mockLinks, wal: mockWAL}
	ctx := context.Background()
	originalURL := "https://example.com"
	shortPath := "/.ABC"

	mockLinks.EXPECT().
		UpSert(ctx, originalURL).
		Return(shortPath, false, nil)

	mockWAL.EXPECT().
		Push(gomock.Any()).
		DoAndReturn(func(e event.Event) error {
			p, _ := event.GetPayload[dto.AddURL](e)
			assert.Equal(t, shortPath, p.ShortURL)
			return nil
		})

	result, err := r.AddURL(ctx, dto.AddURL{OriginalURL: originalURL})

	require.NoError(t, err)
	assert.Equal(t, shortPath, result.ShortURL)
}

func TestRepo_GetURL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	r := &repo{repoLinks: mockLinks}
	ctx := context.Background()
	shortPath := "/.ABC"
	originalURL := "https://example.com"

	mockLinks.EXPECT().
		Select(ctx, shortPath).
		Return(originalURL, false, nil)

	result, err := r.GetURL(ctx, dto.GetURL{ShortURL: shortPath})

	require.NoError(t, err)
	assert.Equal(t, originalURL, result.OriginalURL)
	assert.False(t, result.IsGone)
}

func TestRepo_DeleteURLBySessionID_NotImplementedForInMem(t *testing.T) {
	r := &repo{pgInst: nil}
	ctx := context.Background()

	err := r.DeleteURLBySessionID(ctx, dto.DeleteURLBySessionID{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestRepo_Batch_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	r := &repo{repoLinks: mockLinks}
	ctx := context.Background()
	batch := dto.Batch{
		Batch: []dto.BatchItem{
			{CorrelationID: "1", OriginalURL: "https://example.com/1"},
			{CorrelationID: "2", OriginalURL: "https://example.com/2"},
		},
	}
	expected := dto.Batch{
		Batch: []dto.BatchItem{
			{CorrelationID: "1", ShortURL: "/.ABC"},
			{CorrelationID: "2", ShortURL: "/.DEF"},
		},
	}

	mockLinks.EXPECT().
		BatchUpSert(ctx, batch).
		Return(expected)

	result, err := r.Batch(ctx, batch)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestRepo_GetInternalStats_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStats := NewMockRepoStats(ctrl)
	r := &repo{repoStats: mockStats}
	ctx := context.Background()
	expected := dto.Stats{URLs: 100, Users: 25}

	mockStats.EXPECT().
		GetStats(ctx).
		Return(expected, nil)

	result, err := r.GetInternalStats(ctx)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestRepo_On_EvBatch_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLinks := NewMockRepoLinks(ctrl)
	r := &repo{repoLinks: mockLinks}
	ctx := context.Background()
	batch := dto.Batch{
		Batch: []dto.BatchItem{
			{CorrelationID: "1", OriginalURL: "https://example.com/1"},
		},
	}
	expected := dto.Batch{
		Batch: []dto.BatchItem{
			{CorrelationID: "1", ShortURL: "/.ABC"},
		},
	}
	inputEvent, _ := event.MakeEvent(batch, nil)

	mockLinks.EXPECT().
		BatchUpSert(ctx, batch).
		Return(expected)

	err := r.On(ctx, inputEvent)
	require.NoError(t, err)
}

func TestRepo_PushToWAL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockWAL := NewMockWAL(ctrl)
	r := &repo{wal: mockWAL}
	payload := dto.AddURL{OriginalURL: "https://example.com"}
	e, _ := event.MakeEvent(payload, nil)

	mockWAL.EXPECT().Push(e).Return(nil)

	result, err := r.pushToWAL(e, nil)

	require.NoError(t, err)
	assert.Equal(t, e, result)
}

func TestRepo_PushToWAL_WithError(t *testing.T) {
	r := &repo{}
	testErr := errors.New("test error")

	result, err := r.pushToWAL(nil, testErr)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, testErr, err)
}
