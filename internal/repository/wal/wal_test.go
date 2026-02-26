package wal

import (
	"context"
	"fmt"
	"murl/internal/model/event"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockEvent для тестов
type mockEvent struct {
	Data string
}

func (m mockEvent) Serialize() ([]byte, error) {
	return []byte(m.Data), nil
}

func (m mockEvent) GetID() uuid.UUID {
	uuid, _ := uuid.NewUUID()
	return uuid
}

func TestNewWAL(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.wal")

	t.Run("Success create", func(t *testing.T) {
		w, err := NewWAL(path)
		assert.NoError(t, err)
		assert.NotNil(t, w)
		_ = w.Close()
	})

	t.Run("Empty path error", func(t *testing.T) {
		_, err := NewWAL("")
		assert.Error(t, err)
	})
}

func TestWal_PushAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "push_load.wal")
	ctx := context.Background()

	w, err := NewWAL(path)
	require.NoError(t, err)

	events := []mockEvent{
		{Data: "event_1"},
		{Data: "event_2"},
		{Data: "event_3"},
	}

	t.Run("Push events", func(t *testing.T) {
		for _, e := range events {
			err := w.Push(e)
			assert.NoError(t, err)
		}
		_ = w.Close()
	})

	t.Run("Load events", func(t *testing.T) {
		ch, err := LoadWAL(ctx, path)
		assert.NoError(t, err)

		var received []event.Event
		for e := range ch {
			received = append(received, e)
		}

		assert.Len(t, received, len(events))
		// Здесь предполагается, что event.Parse корректно восстанавливает данные
	})
}

func TestLoadWAL_ContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cancel.wal")

	// Создаем файл с данными
	w, _ := NewWAL(path)
	for i := 0; i < 100; i++ {
		_ = w.Push(mockEvent{Data: fmt.Sprintf("e_%d", i)})
	}
	_ = w.Close()

	t.Run("Cancel middle read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := LoadWAL(ctx, path)
		assert.NoError(t, err)

		// Читаем один элемент и отменяем
		<-ch
		cancel()

		// Канал должен закрыться корректно без зависания горутины
		for range ch {
		}
		assert.True(t, true, "Goroutine exited safely")
	})
}

func TestLoadWAL_NoNewlineAtEnd(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "corrupt.wal")

	// Вручную пишем данные без \n в конце
	err := os.WriteFile(path, []byte("event_data_no_newline"), 0644)
	require.NoError(t, err)

	t.Run("Read last line without newline", func(t *testing.T) {
		ch, err := LoadWAL(context.Background(), path)
		assert.NoError(t, err)

		count := 0
		for range ch {
			count++
		}
		assert.Equal(t, 1, count, "Should process the last line even without newline")
	})
}

func TestWal_Close(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "close.wal")
	w, _ := NewWAL(path)

	t.Run("Double close safety", func(t *testing.T) {
		err := w.Close()
		assert.NoError(t, err)

		// Повторный Close на файле в Go вернет ошибку, но паники быть не должно
		err = w.Close()
		assert.Error(t, err)
	})

	t.Run("Push after close error", func(t *testing.T) {
		err := w.Push(mockEvent{Data: "fail"})
		assert.Error(t, err)
	})
}
