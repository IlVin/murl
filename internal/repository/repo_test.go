package repository

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// mockRepoDataDrv мок для интерфейса IRepoDataDrv
type mockRepoDataDrv struct {
	mock.Mock
}

func (m *mockRepoDataDrv) UpSert(shardID byte, str string) (uint64, error) {
	args := m.Called(shardID, str)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockRepoDataDrv) Select(shardID byte, idx uint64) (string, error) {
	args := m.Called(shardID, idx)
	return args.String(0), args.Error(1)
}

// mockRepoConfig реализует IRepoConfig (включая Zap)
type mockRepoConfig struct {
	drvType   string
	shardSize byte
}

func (m mockRepoConfig) RepoDrv() string  { return m.drvType }
func (m mockRepoConfig) ShardSize() byte  { return m.shardSize }
func (m mockRepoConfig) Zap() *zap.Logger { return zap.NewNop() }

func TestNewRepo(t *testing.T) {
	t.Run("Success initialization", func(t *testing.T) {
		cfg := mockRepoConfig{drvType: "InMemory", shardSize: 64}
		repo := NewRepo(cfg)
		assert.NotNil(t, repo)
		assert.Equal(t, byte(64), repo.shardSize)
	})
}

func TestGetShardID(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		shardSize byte
		want      byte
	}{
		{"Zero shard size", "test", 0, 0},
		{"Single shard", "test", 1, 0},
		{"Consistent hash 1", "url1", 10, 6},
		{"Consistent hash 2", "url2", 10, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GetShardID(tt.key, tt.shardSize))
		})
	}
}

func TestRepo_Save(t *testing.T) {
	t.Run("Success save", func(t *testing.T) {
		mockDB := new(mockRepoDataDrv)
		repo := &Repo{shardSize: 10, db: mockDB, zap: zap.NewNop()}

		url := "https://google.com"
		sID := GetShardID(url, 10)
		mockDB.On("UpSert", sID, url).Return(uint64(1), nil)

		resID, resIdx, err := repo.Save(url)
		assert.NoError(t, err)
		assert.Equal(t, sID, resID)
		assert.Equal(t, uint64(1), resIdx)
	})

	t.Run("Save error", func(t *testing.T) {
		mockDB := new(mockRepoDataDrv)
		repo := &Repo{shardSize: 10, db: mockDB, zap: zap.NewNop()}

		mockDB.On("UpSert", mock.Anything, mock.Anything).Return(uint64(0), errors.New("fail"))
		_, _, err := repo.Save("url")
		assert.ErrorIs(t, err, ErrURLMappingNotSaved)
	})
}

func TestRepo_Load(t *testing.T) {
	t.Run("Success load", func(t *testing.T) {
		mockDB := new(mockRepoDataDrv)
		repo := &Repo{shardSize: 10, db: mockDB, zap: zap.NewNop()}

		mockDB.On("Select", byte(1), uint64(5)).Return("url", nil)
		res, err := repo.Load(1, 5)
		assert.NoError(t, err)
		assert.Equal(t, "url", res)
	})

	t.Run("Load error", func(t *testing.T) {
		mockDB := new(mockRepoDataDrv)
		repo := &Repo{shardSize: 10, db: mockDB, zap: zap.NewNop()}

		mockDB.On("Select", mock.Anything, mock.Anything).Return("", errors.New("fail"))
		_, err := repo.Load(1, 1)
		assert.ErrorIs(t, err, ErrURLNotFound)
	})
}
