package service

import (
	"context"
	"murl/internal/config"
	"murl/internal/model"
	"murl/internal/model/event"
	"net/url"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServiceCfg struct {
	base string
}

func (c testServiceCfg) ShortBaseURL() config.ShortBaseURL {
	u, _ := url.Parse(c.base)
	return config.ShortBaseURL{URL: *u}
}

func TestService_Batch(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockMicroURLRepo(ctrl)
	cfg := testServiceCfg{base: "https://m.url"}
	svc := NewService(context.Background(), cfg, mockRepo)

	t.Run("Mixed valid and invalid URLs", func(t *testing.T) {
		inputPayload := event.PayloadBatch{
			{OrigURL: "https://google.com"}, // Валидный
			{OrigURL: "ftp://wrong.com"},    // Ошибка (схема)
		}
		eInput, _ := event.MakeEvent(inputPayload, nil)

		// Вместо MatchedBy используем DoAndReturn для ручной валидации аргументов
		mockRepo.EXPECT().
			Batch(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, p event.PayloadBatch) (event.PayloadBatch, error) {
				// ПРОВЕРКА: Сервис должен был отфильтровать ftp и оставить только 1 элемент
				assert.Len(t, p, 1, "Service should filter out invalid URLs before calling repo")
				assert.Equal(t, "https://google.com", p[0].OrigURL)

				// Возвращаем результат от лица БД
				return event.PayloadBatch{
					{OrigURL: "https://google.com", ShardID: 1, Idx: 77},
				}, nil
			})

		// Ожидаем запись в WAL (PushEvent)
		mockRepo.EXPECT().PushEvent(gomock.Any(), gomock.Any()).Times(1)

		resEvent, err := svc.Batch(context.Background(), eInput)
		require.NoError(t, err)

		var resPayload event.PayloadBatch
		resEvent.GetPayload(&resPayload)

		assert.Len(t, resPayload, 2)

		var successCount, failCount int
		for _, item := range resPayload {
			if item.Err == errBadURLFormat {
				failCount++
			} else if item.ShortURL != "" {
				successCount++
				// Проверяем, что ссылка собрана верно (Base + ID + Idx)
				assert.Contains(t, item.ShortURL, "https://m.url")
				sID, idx, _ := model.ParseShortURL(item.ShortURL)
				assert.Equal(t, byte(1), sID)
				assert.Equal(t, uint64(77), idx)
			}
		}
		assert.Equal(t, 1, successCount)
		assert.Equal(t, 1, failCount)
	})
}

func TestService_GetURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockMicroURLRepo(ctrl)
	svc := NewService(context.Background(), testServiceCfg{base: "http://m.url"}, mockRepo)

	t.Run("Valid retrieval", func(t *testing.T) {
		// Подготовка тестовых данных через модель
		base, _ := url.Parse("http://m.url")
		sURL, _ := model.MakeShortURL(5, 999, base)

		mockRepo.EXPECT().
			Load(gomock.Any(), byte(5), uint64(999)).
			Return("https://original.io", nil)

		got, err := svc.GetURL(context.Background(), sURL)
		require.NoError(t, err)
		assert.Equal(t, "https://original.io", got)
	})
}

func TestService_Ping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := NewMockMicroURLRepo(ctrl)
	svc := NewService(context.Background(), testServiceCfg{}, mockRepo)

	mockRepo.EXPECT().Ping(gomock.Any()).Return(nil)
	assert.NoError(t, svc.Ping(context.Background()))
}
