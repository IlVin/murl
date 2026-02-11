package repository

import (
	"context"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestNewRepoDrv(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Создаем мок для конфига
	mockCfg := NewMockRepoDrvConfig(ctrl)
	ctx := context.Background()

	t.Run("create InMemory driver", func(t *testing.T) {
		// Настраиваем ожидания: фабрика спросит тип и размер шардов
		mockCfg.EXPECT().RepoDrv().Return("InMemory")
		mockCfg.EXPECT().ShardSize().Return(byte(2))

		drv, err := NewRepoDrv(ctx, mockCfg)

		assert.NoError(t, err)
		assert.NotNil(t, drv)
		// Проверяем, что создался именно InMemory драйвер
		assert.IsType(t, &InMemoryRepoDrv{}, drv)
	})

	t.Run("unknown driver error", func(t *testing.T) {
		mockCfg.EXPECT().RepoDrv().Return("Redis").Times(2) // Неподдерживаемый драйвер

		drv, err := NewRepoDrv(ctx, mockCfg)

		assert.Error(t, err)
		assert.Nil(t, drv)
		assert.Contains(t, err.Error(), "unknown repo driver: Redis")
	})

	t.Run("PgDB initialization failure", func(t *testing.T) {
		// Если выбрать PgDB, фабрика полезет в newPgRepoDrv,
		// которая попытается создать подключение.
		mockCfg.EXPECT().RepoDrv().Return("PgDB")
		mockCfg.EXPECT().ShardSize().Return(byte(1))
		mockCfg.EXPECT().DBDSN().Return("invalid-dsn")

		drv, err := NewRepoDrv(ctx, mockCfg)

		// Ожидаем ошибку инициализации, так как DSN кривой
		assert.Error(t, err)
		assert.Nil(t, drv)
		assert.Contains(t, err.Error(), "failed to init driver PgDB")
	})
}
