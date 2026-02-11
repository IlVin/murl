package repository

import (
	"bufio"
	"context"
	"murl/internal/model/event"
	"os"
	"sync"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCfg реализует RepoConfig для тестов
type mockCfg struct {
	path string
}

func (m mockCfg) RepoDrv() string          { return "InMemory" }
func (m mockCfg) ShardSize() byte          { return 10 }
func (m mockCfg) DBDSN() string            { return "" }
func (m mockCfg) EventStoragePath() string { return m.path }

func TestRepo_Save(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDrv := NewMockRepoDataDrv(ctrl)
	ctx := context.Background()
	longURL := "https://google.com"

	// Ожидаем вызов UpSert в драйвер
	mockDrv.EXPECT().
		UpSert(ctx, gomock.Any(), longURL).
		Return(uint64(100), nil)

	r := &Repo{
		shardSize: 10,
		db:        mockDrv,
		events:    make(chan event.Event, 1),
	}

	sID, idx, err := r.Save(ctx, longURL)

	assert.NoError(t, err)
	assert.Equal(t, uint64(100), idx)
	assert.Equal(t, GetShardID(longURL, 10), sID)

	// Проверяем, что событие улетело в канал
	select {
	case e := <-r.events:
		assert.Equal(t, event.EvAddURL, e.GetType())
	default:
		t.Fatal("event was not pushed to channel")
	}
}

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

func TestRepo_WAL_Integration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDrv := NewMockRepoDataDrv(ctrl)

	// Создаем временный файл
	tmpFile, err := os.CreateTemp("", "wal_*.log")
	require.NoError(t, err)
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	cfg := mockCfg{path: tmpPath}

	// Инициализируем Repo вручную для теста
	r := &Repo{
		shardSize: 10,
		db:        mockDrv,
		events:    make(chan event.Event, 10),
		wg:        sync.WaitGroup{},
	}

	fh, _ := os.OpenFile(tmpPath, os.O_APPEND|os.O_WRONLY, 0644)
	r.walFile = fh
	r.wal = bufio.NewWriter(fh)

	r.wg.Add(1)
	go r.eventSaver(cfg, r.events)

	// Эмулируем событие
	p := event.PayloadAddURL{URL: "https://test.com", ShardID: 1, ID: 1}
	e, _ := event.MakeEvent(p, nil)
	r.events <- e

	// Даем время на запись и закрываем
	time.Sleep(100 * time.Millisecond)
	r.Close()

	// Проверяем, что в файле есть данные
	data, err := os.ReadFile(tmpPath)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)
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
