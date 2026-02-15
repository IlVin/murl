package repository

import (
	"context"
	"murl/internal/model/event"
	"os"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

// mockCfg реализует RepoConfig для тестов
type mockCfg struct {
	path string
}

func (m mockCfg) RepoDrv() string          { return "InMemory" }
func (m mockCfg) ShardSize() byte          { return 10 }
func (m mockCfg) DBDSN() string            { return "" }
func (m mockCfg) EventStoragePath() string { return m.path }

func TestRepo_Batch(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDrv := NewMockRepoDataDrv(ctrl)
	shardSize := byte(10)
	r := &Repo{shardSize: shardSize, db: mockDrv}

	payload := event.PayloadBatch{
		{OrigURL: "https://a.com"},
		{OrigURL: "https://b.com"},
	}

	// Ожидаем вызов. Вместо NotZero проверяем корректность расчета
	mockDrv.EXPECT().
		BatchUpSert(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, b []event.PayloadBatchItem) ([]event.PayloadBatchItem, error) {
			for i := range b {
				// ПРОВЕРКА: рассчитываем ожидаемый ID заново в тесте
				expectedID := GetShardID(b[i].OrigURL, shardSize)
				assert.Equal(t, expectedID, b[i].ShardID, "ShardID mismatch for URL: %s", b[i].OrigURL)
			}
			return b, nil
		})

	res, err := r.Batch(context.Background(), payload)
	assert.NoError(t, err)
	assert.Len(t, res, 2)
}

func TestRepo_LoadStoredEvents(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDrv := NewMockRepoDataDrv(ctrl)

	// Подготовка файла с одним событием
	tmpFile, _ := os.CreateTemp("", "wal_load_*.log")
	defer os.Remove(tmpFile.Name())

	p := event.PayloadAddURL{ShardID: 5, ID: 500, URL: "https://restore.me"}
	e, _ := event.MakeEvent(p, nil)
	data, _ := e.Serialize()

	tmpFile.Write(data)
	tmpFile.Write([]byte("\n"))
	tmpFile.Close()

	// Ожидаем вызов восстановления в драйвер
	mockDrv.EXPECT().
		Set(gomock.Any(), byte(5), uint64(500), "https://restore.me").
		Return(nil)

	cfg := mockCfg{path: tmpFile.Name()}
	loadStoredEvents(context.Background(), cfg, mockDrv)
}

func TestGetShardID_ZeroSize(t *testing.T) {
	// Защита от деления на ноль
	assert.Equal(t, byte(0), GetShardID("test", 0))
}
