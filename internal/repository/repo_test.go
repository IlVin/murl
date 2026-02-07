package repository

import (
	"fmt"
	"murl/internal/model/event"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

type mockRepoConfig struct {
	drv       string
	shardSize byte
	path      string
	dbdsn     string
}

func (m *mockRepoConfig) DBDSN() string            { return m.dbdsn }
func (m *mockRepoConfig) RepoDrv() string          { return m.drv }
func (m *mockRepoConfig) ShardSize() byte          { return m.shardSize }
func (m *mockRepoConfig) EventStoragePath() string { return m.path }

type mockDataDrv struct {
	mock.Mock
}

func (m *mockDataDrv) UpSert(shardID byte, str string) (uint64, error) {
	args := m.Called(shardID, str)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockDataDrv) Select(shardID byte, idx uint64) (string, error) {
	args := m.Called(shardID, idx)
	return args.String(0), args.Error(1)
}

func (m *mockDataDrv) Set(shardID byte, idx uint64, u string) error {
	args := m.Called(shardID, idx, u)
	return args.Error(0)
}

// --- Tests ---

func TestGetShardID(t *testing.T) {
	// Проверка корректности хеширования и деления на шарды
	assert.Equal(t, byte(0), GetShardID("any", 0))
	assert.Equal(t, byte(0), GetShardID("test", 1))

	// Разные строки должны (вероятно) попадать в разные шарды при большом количестве шардов
	s1 := GetShardID("url1", 64)
	s2 := GetShardID("url2", 64)
	assert.NotEqual(t, s1, s2, "Хеш должен распределять значения")
}

func TestRepo_BasicOperations(t *testing.T) {
	mDrv := new(mockDataDrv)
	cfg := &mockRepoConfig{drv: "InMemory", shardSize: 10, path: ""}

	// Инициализируем репо
	r := NewRepo(cfg)
	r.db = mDrv // Подменяем на мок
	defer r.Close()

	url := "https://google.com"
	sID := GetShardID(url, 10)

	t.Run("Save Success", func(t *testing.T) {
		mDrv.On("UpSert", sID, url).Return(uint64(100), nil).Once()

		resSID, resIdx, err := r.Save(url)
		require.NoError(t, err)
		assert.Equal(t, sID, resSID)
		assert.Equal(t, uint64(100), resIdx)
	})

	t.Run("Load Success", func(t *testing.T) {
		mDrv.On("Select", sID, uint64(100)).Return(url, nil).Once()

		resURL, err := r.Load(sID, uint64(100))
		require.NoError(t, err)
		assert.Equal(t, url, resURL)
	})

	t.Run("Save DB Error", func(t *testing.T) {
		mDrv.On("UpSert", mock.Anything, mock.Anything).Return(uint64(0), fmt.Errorf("db fail")).Once()
		_, _, err := r.Save("http://error.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save URL mapping")
	})

	t.Run("Load Not Found", func(t *testing.T) {
		mDrv.On("Select", mock.Anything, mock.Anything).Return("", fmt.Errorf("not found")).Once()
		_, err := r.Load(0, 999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "URL not found")
	})
}

func TestRepo_EventStorage_SuccessCycle(t *testing.T) {
	tmpDir := t.TempDir()
	eventFile := filepath.Join(tmpDir, "events.log")

	cfg := &mockRepoConfig{drv: "InMemory", shardSize: 10, path: eventFile}
	mDrv := new(mockDataDrv)

	// 1. Создаем репо и записываем событие
	r := NewRepo(cfg)
	r.db = mDrv

	testURL := "http://example.com"
	sID := GetShardID(testURL, 10)

	mDrv.On("UpSert", sID, testURL).Return(uint64(50), nil).Once()

	_, _, err := r.Save(testURL)
	require.NoError(t, err)

	// Закрываем, чтобы saver сбросил данные в файл
	r.Close()
	time.Sleep(50 * time.Millisecond)

	// 2. Проверяем восстановление данных из файла в новый репо
	mDrvRestored := new(mockDataDrv)
	// Ожидаем, что LoadStoredEvents вызовет Set для восстановления состояния
	mDrvRestored.On("Set", sID, uint64(50), testURL).Return(nil).Once()

	// Создаем объект вручную, чтобы вызвать LoadStoredEvents до запуска saver
	r2 := &Repo{
		shardSize: 10,
		db:        mDrvRestored,
		events:    make(chan event.Event, 1),
	}
	r2.LoadStoredEvents(cfg)

	mDrvRestored.AssertExpectations(t)
}

func TestRepo_LoadStoredEvents_Errors(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("Missing File", func(t *testing.T) {
		cfg := &mockRepoConfig{path: filepath.Join(tmpDir, "non_existent.log")}
		r := &Repo{}
		// Не должно паниковать, просто выведет ERROR в лог
		r.LoadStoredEvents(cfg)
	})

	t.Run("Empty Path", func(t *testing.T) {
		cfg := &mockRepoConfig{path: ""}
		r := &Repo{}
		r.LoadStoredEvents(cfg) // Должен просто выйти
	})

	t.Run("Corrupted File", func(t *testing.T) {
		path := filepath.Join(tmpDir, "corrupt.log")
		// Пишем невалидные данные (не JSON)
		os.WriteFile(path, []byte("invalid_data\n"), 0644)

		cfg := &mockRepoConfig{path: path}
		r := &Repo{db: new(mockDataDrv)}
		r.LoadStoredEvents(cfg) // Пройдет через ошибки парсинга
	})
}

func TestEventSaver_SpecialCases(t *testing.T) {
	t.Run("No Storage Path", func(t *testing.T) {
		cfg := &mockRepoConfig{path: ""}
		ch := make(chan event.Event, 1)

		p := event.PayloadAddURL{URL: "test"}
		ev, _ := event.MakeEvent(p)
		ch <- ev
		close(ch)

		// Должен просто вычитать канал и завершиться
		eventSaver(cfg, ch)
	})

	t.Run("File Write Error (Invalid Path)", func(t *testing.T) {
		// Путь к директории как к файлу вызовет ошибку открытия
		cfg := &mockRepoConfig{path: "/"}
		ch := make(chan event.Event)

		assert.Panics(t, func() {
			eventSaver(cfg, ch)
		})
	})
}
