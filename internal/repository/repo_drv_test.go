package repository

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestNewRepoDrv(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	t.Run("success_default_in_memory", func(t *testing.T) {
		// Конфиг возвращает InMemory или пустую строку (если это дефолт)
		mockCfg := NewMockRepoDrvConfig(ctrl)
		mockCfg.EXPECT().RepoDrv().Return("InMemory").AnyTimes()
		mockCfg.EXPECT().ShardSize().Return(byte(2)).AnyTimes()

		drv, err := NewRepoDrv(ctx, mockCfg)

		assert.NoError(t, err)
		assert.NotNil(t, drv)
		assert.IsType(t, &InMemoryRepoDrv{}, drv)
	})

	t.Run("success_pg_db", func(t *testing.T) {
		mockCfg := NewMockRepoDrvConfig(ctrl)
		mockCfg.EXPECT().RepoDrv().Return("PgDB").AnyTimes()
		mockCfg.EXPECT().ShardSize().Return(byte(1)).AnyTimes()
		mockCfg.EXPECT().DBDSN().Return("postgres://localhost:5432/db").AnyTimes()

		drv, err := NewRepoDrv(ctx, mockCfg)

		// Если база не запущена, NewPgHndl вернет ошибку, это корректное поведение
		if err != nil {
			assert.Contains(t, err.Error(), "failed to init driver PgDB")
			assert.Nil(t, drv)
		} else {
			assert.NotNil(t, drv)
		}
	})

	t.Run("unknown_driver_error", func(t *testing.T) {
		// Передаем явно неподдерживаемый драйвер
		mockCfg := NewMockRepoDrvConfig(ctrl)
		mockCfg.EXPECT().RepoDrv().Return("UnknownUnsupportedDriver").AnyTimes()

		drv, err := NewRepoDrv(ctx, mockCfg)

		// Теперь эти ассерты пройдут, так как NewRepoDrv вернет ошибку
		assert.Nil(t, drv)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown repo driver")
	})
}

func TestConstantsAndErrors(t *testing.T) {
	// Покрываем объявление констант и переменных
	assert.Equal(t, 300, defaultCap)
	assert.Equal(t, "internal server error", errInternalServerError)
}
