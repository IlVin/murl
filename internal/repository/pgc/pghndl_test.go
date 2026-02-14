package pgc

import (
	"context"
	"errors"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type PgHndlSuite struct {
	suite.Suite
	ctrl     *gomock.Controller
	mockProv *MockpgPoolProvider
}

func (s *PgHndlSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.mockProv = NewMockpgPoolProvider(s.ctrl)
}

func (s *PgHndlSuite) TearDownTest() {
	s.ctrl.Finish()
}

func TestPgHndlSuite(t *testing.T) {
	suite.Run(t, new(PgHndlSuite))
}

// 1. Тест инициализации
func (s *PgHndlSuite) TestNewPgHndl_Fail() {
	ctx := context.Background()
	// Невалидная строка подключения вызовет ошибку в pgxpool.New
	h, err := NewPgHndl(ctx, "test", "invalid-conn-string")
	assert.Error(s.T(), err)
	assert.Nil(s.T(), h)
}

// 2. Тест Circuit Breaker (HandleError)
func (s *PgHndlSuite) TestHandleError_Transitions() {
	h := &PgHndl{
		failures: NewFailureCounter(2, time.Hour),
	}
	h.isReady.Store(true)

	// Первая ошибка — остаемся Online
	h.HandleError(errors.New("fail 1"))
	assert.True(s.T(), h.IsReady())

	// Вторая ошибка — выбивает предохранитель
	h.HandleError(errors.New("fail 2"))
	assert.False(s.T(), h.IsReady())

	// Успех — возвращаемся в Online
	h.HandleError(nil)
	assert.True(s.T(), h.IsReady())
}

// 3. Тест механизма Probing (CanTry)
func (s *PgHndlSuite) TestCanTry() {
	h := &PgHndl{
		failures: NewFailureCounter(1, time.Hour),
	}
	h.Offline()

	// Первый вызов в Offline — можно (проба)
	assert.True(s.T(), h.CanTry())

	// Второй вызов сразу же — нельзя (уже пробуем или ждем 5 сек)
	assert.False(s.T(), h.CanTry())

	// Имитируем проход 6 секунд
	h.lastRetry.Store(time.Now().Unix() - 6)
	assert.True(s.T(), h.CanTry())
}

// 4. Тест Транзакции (Успех)
func (s *PgHndlSuite) TestTx_Success() {
	h := &PgHndl{
		pgPoolProv: s.mockProv,
		failures:   NewFailureCounter(3, time.Hour),
		repeater:   NewPgBackoff(1, time.Second),
	}
	h.Online()

	mockTx := NewMockTx(s.ctrl) // Предполагается наличие мока для pgx.Tx

	s.mockProv.EXPECT().Begin(gomock.Any()).Return(mockTx, nil)
	mockTx.EXPECT().Commit(gomock.Any()).Return(nil)

	err := h.Tx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return nil
	})

	assert.NoError(s.T(), err)
}

// 5. Тест Транзакции (Паника)
func (s *PgHndlSuite) TestTx_Panic() {
	h := &PgHndl{
		pgPoolProv: s.mockProv,
		failures:   NewFailureCounter(3, time.Hour),
		repeater:   NewPgBackoff(1, time.Second),
	}
	h.Online()

	mockTx := NewMockTx(s.ctrl)
	s.mockProv.EXPECT().Begin(gomock.Any()).Return(mockTx, nil)
	mockTx.EXPECT().Rollback(gomock.Any()).Return(nil)

	err := h.Tx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		panic("boom")
	})

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(), "panic recovered")
}

// 6. Тест закрытия
func (s *PgHndlSuite) TestClose() {
	h := &PgHndl{
		pgPoolProv: s.mockProv,
	}
	h.Online()

	s.mockProv.EXPECT().Close().Times(1)

	h.Close()
	assert.True(s.T(), h.isClosed.Load())
	assert.False(s.T(), h.IsReady())
}

// 7. Тест Ping
func (s *PgHndlSuite) TestPing() {
	h := &PgHndl{
		pgPoolProv: s.mockProv,
		failures:   NewFailureCounter(1, time.Hour),
	}

	s.mockProv.EXPECT().Ping(gomock.Any()).Return(errors.New("ping fail"))

	err := h.Ping(context.Background())
	assert.Error(s.T(), err)
	assert.False(s.T(), h.IsReady())
}
