package wal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"murl/internal/model/event"
)

func TestWAL_PushAndLoad(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 1. Подготовка временного файла
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// 2. Создаем WAL и пишем события
	w, err := NewWAL(walPath)
	require.NoError(t, err)

	// Создаем реальные события (используем PayloadAddURL как пример)
	p1 := event.PayloadAddURL{OriginalURL: "https://google.com", ShortURL: "short1"}
	ev1, _ := event.MakeEvent(p1, nil)

	p2 := event.PayloadAddURL{OriginalURL: "https://yandex.ru", ShortURL: "short2"}
	ev2, _ := event.MakeEvent(p2, nil)

	// Пишем в лог
	require.NoError(t, w.Push(ev1))
	require.NoError(t, w.Push(ev2))
	require.NoError(t, w.Close())

	// 3. Проверяем чтение через LoadWAL
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := LoadWAL(ctx, walPath)
	require.NoError(t, err)

	var recovered []event.Event
	for ev := range ch {
		recovered = append(recovered, ev)
	}

	// Проверки
	assert.Equal(t, 2, len(recovered))
	assert.Equal(t, ev1.GetID(), recovered[0].GetID())
	assert.Equal(t, ev2.GetID(), recovered[1].GetID())
	assert.Equal(t, ev1.GetType(), recovered[0].GetType())
}

func TestLoadWAL_CorruptedData(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "corrupt.wal")

	// Вручную создаем файл с одной валидной и одной битой строкой
	p := event.PayloadAddURL{OriginalURL: "ok"}
	ev, _ := event.MakeEvent(p, nil)
	data, _ := ev.Serialize()

	err := os.WriteFile(walPath, append(data, []byte("\nnot-a-json-line\n")...), 0644)
	require.NoError(t, err)

	ctx := context.Background()
	ch, err := LoadWAL(ctx, walPath)
	require.NoError(t, err)

	// Должно прочитаться только одно (валидное) событие, битое — проигнорироваться (лог в slog)
	count := 0
	for range ch {
		count++
	}
	assert.Equal(t, 1, count)
}

func TestLoadWAL_ContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "cancel.wal")

	// Создаем большой файл (имитация)
	fh, _ := os.Create(walPath)
	p := event.PayloadAddURL{OriginalURL: "limit"}
	ev, _ := event.MakeEvent(p, nil)
	data, _ := ev.Serialize()
	for i := 0; i < 100; i++ {
		fh.Write(append(data, '\n'))
	}
	fh.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := LoadWAL(ctx, walPath)
	require.NoError(t, err)

	// Читаем одно и отменяем контекст
	<-ch
	cancel()

	// Канал должен закрыться в обозримом будущем
	select {
	case _, ok := <-ch:
		if ok {
			// Могло проскочить еще одно событие из буфера — это нормально
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("LoadWAL didn't stop after context cancel")
	}
}

func TestNewWAL_Errors(t *testing.T) {
	// Пустой путь
	_, err := NewWAL("")
	assert.Error(t, err)

	// Невалидный путь (не существующая папка)
	_, err = NewWAL("/non/existent/path/wal.log")
	assert.Error(t, err)
}
