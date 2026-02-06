package repository

import (
	"errors"
	"murl/internal/model/event"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---

type MockRepoConfig struct {
	mock.Mock
}

func (m *MockRepoConfig) RepoDrv() string          { return m.Called().String(0) }
func (m *MockRepoConfig) ShardSize() byte          { return m.Called().Get(0).(byte) }
func (m *MockRepoConfig) EventStoragePath() string { return m.Called().String(0) }

type MockDrv struct {
	mock.Mock
}

func (m *MockDrv) UpSert(shardID byte, str string) (uint64, error) {
	args := m.Called(shardID, str)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *MockDrv) Select(shardID byte, idx uint64) (string, error) {
	args := m.Called(shardID, idx)
	return args.String(0), args.Error(1)
}

func (m *MockDrv) Set(shardID byte, idx uint64, u string) error {
	return m.Called(shardID, idx, u).Error(0)
}

// --- Tests ---

func TestGetShardID(t *testing.T) {
	assert.Equal(t, byte(0), GetShardID("test", 0))
	assert.NotEqual(t, byte(0), GetShardID("url", 64))
}

func TestRepo_BasicOperations(t *testing.T) {
	drv := new(MockDrv)
	repo := &Repo{
		shardSize: 64,
		db:        drv,
		events:    make(chan event.Event, 10),
	}

	t.Run("Save Success", func(t *testing.T) {
		url := "https://google.com"
		sID := GetShardID(url, 64)
		drv.On("UpSert", sID, url).Return(uint64(1), nil).Once()

		sid, idx, err := repo.Save(url)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), idx)
		assert.Equal(t, sID, sid)
	})

	t.Run("Save DB Error", func(t *testing.T) {
		drv.On("UpSert", mock.Anything, mock.Anything).Return(uint64(0), errors.New("db fail")).Once()
		_, _, err := repo.Save("fail")
		assert.ErrorIs(t, err, ErrURLMappingNotSaved)
	})

	t.Run("Load Success", func(t *testing.T) {
		drv.On("Select", byte(1), uint64(1)).Return("url", nil).Once()
		res, err := repo.Load(1, 1)
		assert.NoError(t, err)
		assert.Equal(t, "url", res)
	})

	t.Run("Load Error", func(t *testing.T) {
		drv.On("Select", mock.Anything, mock.Anything).Return("", errors.New("not found")).Once()
		_, err := repo.Load(1, 1)
		assert.ErrorIs(t, err, ErrURLNotFound)
	})

	t.Run("Set Success", func(t *testing.T) {
		drv.On("Set", byte(1), uint64(1), "url").Return(nil).Once()
		err := repo.Set(1, 1, "url")
		assert.NoError(t, err)
	})
}

func TestEventSaver_Scenarios(t *testing.T) {
	t.Run("Empty path - drain channel", func(t *testing.T) {
		cfg := new(MockRepoConfig)
		cfg.On("EventStoragePath").Return("")

		ch := make(chan event.Event, 1)
		p := event.PayloadAddURL{}
		ev, _ := event.MakeEvent(p)
		ch <- ev
		close(ch)

		// Должно просто вычитать канал без паник и файлов
		eventSaver(cfg, ch)
	})

	t.Run("Invalid path - panic", func(t *testing.T) {
		cfg := new(MockRepoConfig)
		cfg.On("EventStoragePath").Return("/non/existent/path/file.log")
		ch := make(chan event.Event)
		assert.Panics(t, func() { eventSaver(cfg, ch) })
	})

	t.Run("Serialization Error", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "events.log")
		cfg := new(MockRepoConfig)
		cfg.On("EventStoragePath").Return(tmp)

		ch := make(chan event.Event, 1)
		// Используем пустое событие или мок, чтобы вызвать ошибку Serialize
		// В вашей реализации pvtEvent Serialize падает редко, но мы проверим ветку лога
		ev := &brokenEvent{}
		ch <- ev
		close(ch)

		eventSaver(cfg, ch)
		content, _ := os.ReadFile(tmp)
		assert.Equal(t, 0, len(content))
	})
}

func TestRecovery_Scenarios(t *testing.T) {
	t.Run("No path - return", func(t *testing.T) {
		cfg := new(MockRepoConfig)
		cfg.On("EventStoragePath").Return("")
		repo := &Repo{}
		repo.LoadStoredEvents(cfg) // Не должно упасть
	})

	t.Run("File not found - log error", func(t *testing.T) {
		cfg := new(MockRepoConfig)
		cfg.On("EventStoragePath").Return("not_exist.log")
		repo := &Repo{}
		repo.LoadStoredEvents(cfg) // Логирует ошибку и выходит
	})

	t.Run("Successful Recovery", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "recovery.log")
		// Готовим файл с одним событием
		p := event.PayloadAddURL{
			URL:     "http://test.com",
			ID:      10,
			ShardID: 1,
		}
		ev, _ := event.MakeEvent(p)
		data, _ := ev.Serialize()
		os.WriteFile(tmp, append(data, 0x0A), 0666)

		cfg := new(MockRepoConfig)
		cfg.On("EventStoragePath").Return(tmp)

		drv := new(MockDrv)
		drv.On("Set", byte(1), uint64(10), "http://test.com").Return(nil).Once()

		repo := &Repo{db: drv}
		repo.LoadStoredEvents(cfg)
		drv.AssertExpectations(t)
	})
}

func TestNewRepo_Functional(t *testing.T) {
	// Мы не можем легко мокать NewRepoDrv, так как это свободная функция,
	// поэтому полагаемся на конфиг InMemory
	tmp := filepath.Join(t.TempDir(), "newrepo.log")
	cfg := new(MockRepoConfig)
	cfg.On("RepoDrv").Return("InMemory")
	cfg.On("ShardSize").Return(byte(16))
	cfg.On("EventStoragePath").Return(tmp)

	repo := NewRepo(cfg)
	assert.NotNil(t, repo)

	// Проверяем Save и закрытие
	repo.Save("http://final.com")
	repo.Close()

	// Даем время горутине дописать
	time.Sleep(50 * time.Millisecond)
	content, _ := os.ReadFile(tmp)
	assert.NotEmpty(t, content)
}

// Вспомогательный тип для ошибки сериализации
type brokenEvent struct{ event.Event }

func (b *brokenEvent) Serialize() ([]byte, error) { return nil, errors.New("fail") }
