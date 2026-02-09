package repository

import (
	"bufio"
	"context"
	"errors"
	"os"
	"testing"

	"murl/internal/model/event"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetShardID(t *testing.T) {
	t.Run("zero shard size", func(t *testing.T) {
		assert.Equal(t, byte(0), GetShardID("test", 0))
	})
	t.Run("consistent hashing", func(t *testing.T) {
		key := "http://example.com"
		id1 := GetShardID(key, 10)
		id2 := GetShardID(key, 10)
		assert.Equal(t, id1, id2)
		assert.Less(t, id1, byte(10))
	})
}

func TestRepo_Methods(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDrv := NewMockRepoDataDrv(ctrl)
	ctx := context.Background()

	r := &Repo{
		shardSize: 10,
		db:        mockDrv,
		events:    make(chan event.Event, 10),
	}

	t.Run("Save success", func(t *testing.T) {
		url := "https://google.com"
		// Ожидаем вызов UpSert в драйвер
		mockDrv.EXPECT().UpSert(ctx, gomock.Any(), url).Return(uint64(123), nil)

		sID, id, err := r.Save(ctx, url)
		assert.NoError(t, err)
		assert.Equal(t, uint64(123), id)

		// Проверяем, что событие улетело в канал
		evt := <-r.events
		assert.Equal(t, event.EvAddURL, evt.GetType())

		var p event.PayloadAddURL
		err = evt.GetPayload(&p)
		assert.NoError(t, err)
		assert.Equal(t, sID, p.ShardID)
	})

	t.Run("Save driver error", func(t *testing.T) {
		mockDrv.EXPECT().UpSert(ctx, gomock.Any(), gomock.Any()).Return(uint64(0), errors.New("db crash"))
		_, _, err := r.Save(ctx, "fail")
		assert.Error(t, err)
	})

	t.Run("Load success", func(t *testing.T) {
		mockDrv.EXPECT().Select(ctx, byte(1), uint64(1)).Return("https://yandex.ru", nil)
		res, err := r.Load(ctx, 1, 1)
		assert.NoError(t, err)
		assert.Equal(t, "https://yandex.ru", res)
	})

	t.Run("Set success", func(t *testing.T) {
		mockDrv.EXPECT().Set(ctx, byte(1), uint64(1), "new").Return(nil)
		err := r.Set(ctx, 1, 1, "new")
		assert.NoError(t, err)
	})

	t.Run("Ping error - no hndl", func(t *testing.T) {
		err := r.Ping(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database was not set")
	})
}

func TestLoadStoredEvents(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCfg := NewMockRepoConfig(ctrl)
	mockDrv := NewMockRepoDrv(ctrl)
	ctx := context.Background()

	t.Run("empty path - return", func(t *testing.T) {
		mockCfg.EXPECT().EventStoragePath().Return("")
		loadStoredEvents(ctx, mockCfg, mockDrv)
	})

	t.Run("file exists - process", func(t *testing.T) {
		tmpFile := "test_events.log"
		defer os.Remove(tmpFile)

		// Генерируем реальное событие для парсинга
		p := event.PayloadAddURL{ShardID: 1, ID: 55, URL: "http://saved.com"}
		evt, _ := event.MakeEvent(p)
		data, _ := evt.Serialize()

		err := os.WriteFile(tmpFile, append(data, '\n'), 0644)
		require.NoError(t, err)

		mockCfg.EXPECT().EventStoragePath().Return(tmpFile).AnyTimes()
		mockDrv.EXPECT().Set(ctx, byte(1), uint64(55), "http://saved.com").Return(nil)

		loadStoredEvents(ctx, mockCfg, mockDrv)
	})
}

func TestEventSaver(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockCfg := NewMockRepoConfig(ctrl)

	t.Run("drain channel if path empty", func(t *testing.T) {
		mockCfg.EXPECT().EventStoragePath().Return("")
		ch := make(chan event.Event, 1)

		p := event.PayloadAddURL{URL: "test"}
		evt, _ := event.MakeEvent(p)
		ch <- evt
		close(ch)

		// Должен вычитать и выйти без паники
		eventSaver(mockCfg, ch)
	})

	t.Run("write to file", func(t *testing.T) {
		tmpFile := "saver.log"
		defer os.Remove(tmpFile)

		mockCfg.EXPECT().EventStoragePath().Return(tmpFile).AnyTimes()
		ch := make(chan event.Event, 1)

		p := event.PayloadAddURL{URL: "http://logged.com"}
		evt, _ := event.MakeEvent(p)
		ch <- evt
		close(ch)

		eventSaver(mockCfg, ch)

		// Проверяем содержимое
		f, _ := os.Open(tmpFile)
		defer f.Close()
		scanner := bufio.NewScanner(f)
		assert.True(t, scanner.Scan())
		assert.Contains(t, scanner.Text(), "http://logged.com")
	})
}

func TestRepo_Close(t *testing.T) {
	r := &Repo{events: make(chan event.Event)}
	r.Close()

	// Проверяем закрытие канала
	_, ok := <-r.events
	assert.False(t, ok)
}
