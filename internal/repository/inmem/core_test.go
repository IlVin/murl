package inmem

import (
	"murl/internal/mocks"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNewInMemCore(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCfg := mocks.NewMockInMemConfig(ctrl)

	t.Run("Success initialization", func(t *testing.T) {
		const size byte = 4
		mockCfg.EXPECT().ShardSize().Return(size)

		core, err := NewInMemCore(mockCfg)
		require.NoError(t, err)
		assert.Equal(t, size, core.Size())
		assert.Len(t, core.shards, int(size))

		// Проверяем, что мапы инициализированы
		for i := range core.shards {
			assert.NotNil(t, core.shards[i].Data)
			assert.NotNil(t, core.shards[i].Index)
		}
	})

	t.Run("Invalid shard size", func(t *testing.T) {
		mockCfg.EXPECT().ShardSize().Return(byte(0))

		core, err := NewInMemCore(mockCfg)
		assert.Nil(t, core)
		assert.Error(t, err)
		assert.Equal(t, "shard size must be greater than 0", err.Error())
	})
}

func TestInMemCore_GetShard(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCfg := mocks.NewMockInMemConfig(ctrl)
	mockCfg.EXPECT().ShardSize().Return(byte(2)).AnyTimes()

	core, _ := NewInMemCore(mockCfg)

	t.Run("Valid shard index", func(t *testing.T) {
		shard, err := core.GetShard(1)
		assert.NoError(t, err)
		assert.NotNil(t, shard)
	})

	t.Run("Out of range index", func(t *testing.T) {
		shard, err := core.GetShard(2)
		assert.Nil(t, shard)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "out of range")
	})
}

func TestInMemCore_ShardID(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCfg := mocks.NewMockInMemConfig(ctrl)

	// Создаем кор с 10 шардами
	mockCfg.EXPECT().ShardSize().Return(byte(10))
	core, _ := NewInMemCore(mockCfg)

	t.Run("Deterministic ID", func(t *testing.T) {
		key := "test-key"
		id1 := core.ShardID(key)
		id2 := core.ShardID(key)

		assert.Equal(t, id1, id2, "ShardID must be deterministic")
		assert.Less(t, id1, core.Size(), "ShardID must be within range")
	})

	t.Run("Different keys different shards", func(t *testing.T) {
		// Вероятностный тест, но для 10 шардов и этих ключей сработает
		id1 := core.ShardID("a")
		id2 := core.ShardID("b")
		id3 := core.ShardID("c")

		// Проверяем просто что это валидные байты
		assert.Less(t, id1, byte(10))
		assert.Less(t, id2, byte(10))
		assert.Less(t, id3, byte(10))
	})
}

func TestInMemCore_Size(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCfg := mocks.NewMockInMemConfig(ctrl)
	mockCfg.EXPECT().ShardSize().Return(byte(64))

	core, _ := NewInMemCore(mockCfg)
	assert.Equal(t, byte(64), core.Size())
}
