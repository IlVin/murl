package pgc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// --- Вспомогательные моки для pgx.Tx (mockgen не всегда удобно генерирует внешние типы) ---

type mockTx struct {
	pgx.Tx
	rollbackFn func()
	commitFn   func() error
}

func (m *mockTx) Rollback(ctx context.Context) error { m.rollbackFn(); return nil }
func (m *mockTx) Commit(ctx context.Context) error   { return m.commitFn() }

// --- Настройка Suite ---

type PgHndlTestSuite struct {
	suite.Suite
	ctrl *gomock.Controller
	mock *MockpgPoolProvider
	h    *PgHndl
}

func (s *PgHndlTestSuite) SetupTest() {
	s.ctrl = gomock.NewController(s.T())
	s.mock = NewMockpgPoolProvider(s.ctrl)
	s.h = &PgHndl{
		name:       "test-db",
		host:       "localhost:5432",
		pgPoolProv: s.mock,
	}
	s.h.isReady.Store(true)
	// Сбрасываем кэш пинга в прошлое
	s.h.lastCheckResult.Store(checkResult{timestamp: time.Now().Add(-10 * time.Second)})
}

func TestPgHndlSuite(t *testing.T) {
	suite.Run(t, new(PgHndlTestSuite))
}

// --- Тесты логики ---

func (s *PgHndlTestSuite) TestIsNetworkError() {
	s.Run("nil error", func() {
		assert.False(s.T(), IsNetworkError(nil))
	})
	s.Run("net.Error", func() {
		err := &net.OpError{Op: "read"}
		assert.True(s.T(), IsNetworkError(err))
	})
	s.Run("pgconn.ConnectError", func() {
		err := &pgconn.ConnectError{}
		assert.True(s.T(), IsNetworkError(err))
	})
	s.Run("pgconn.PgError 08 class", func() {
		err := &pgconn.PgError{Code: "08001"}
		assert.True(s.T(), IsNetworkError(err))
	})
	s.Run("pgconn.PgError other class", func() {
		err := &pgconn.PgError{Code: "23505"}
		assert.False(s.T(), IsNetworkError(err))
	})
}

func (s *PgHndlTestSuite) TestPing_CachingAndLocking() {
	s.Run("Successful Ping updates state", func() {
		s.mock.EXPECT().Ping(gomock.Any()).Return(nil)
		err := s.h.Ping(context.Background())
		assert.NoError(s.T(), err)
		assert.True(s.T(), s.h.IsReady())
	})

	s.Run("Cached result", func() {
		// Время последнего пинга — сейчас, Ping не должен вызываться у провайдера
		s.h.lastCheckResult.Store(checkResult{timestamp: time.Now(), err: nil})
		err := s.h.Ping(context.Background())
		assert.NoError(s.T(), err)
	})

	s.Run("Locking (TryLock)", func() {
		s.h.lastCheckResult.Store(checkResult{timestamp: time.Now().Add(-10 * time.Second)})
		s.h.mu.Lock() // Симулируем другой процесс пинга
		defer s.h.mu.Unlock()

		err := s.h.Ping(context.Background())
		assert.NoError(s.T(), err, "Should return nil if check in progress")
	})
}

func (s *PgHndlTestSuite) TestTx_Success() {
	mTx := &mockTx{commitFn: func() error { return nil }}
	s.mock.EXPECT().Begin(gomock.Any()).Return(mTx, nil)

	err := s.h.Tx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return nil
	})
	assert.NoError(s.T(), err)
}

func (s *PgHndlTestSuite) TestTx_PanicRecovery() {
	mTx := &mockTx{rollbackFn: func() {}}
	s.mock.EXPECT().Begin(gomock.Any()).Return(mTx, nil)

	err := s.h.Tx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		panic("disaster")
	})

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(), "panic recovered")
}

func (s *PgHndlTestSuite) TestTx_NetworkErrorHandling() {
	s.mock.EXPECT().Begin(gomock.Any()).Return(nil, &net.OpError{Op: "dial"})

	err := s.h.Tx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return nil
	})

	assert.Error(s.T(), err)
	assert.False(s.T(), s.h.IsReady(), "Should go offline on net error")
}

func (s *PgHndlTestSuite) TestPgPool_Methods() {
	s.Run("Normal execution", func() {
		err := s.h.PgPool(context.Background(), func(ctx context.Context, p *pgxpool.Pool) error {
			return nil
		})
		assert.NoError(s.T(), err)
	})

	s.Run("Offline flow", func() {
		s.h.Offline()
		s.mock.EXPECT().Ping(gomock.Any()).Return(errors.New("db dead"))
		err := s.h.PgPool(context.Background(), nil)
		assert.Error(s.T(), err)
	})
}

func (s *PgHndlTestSuite) TestClose() {
	s.mock.EXPECT().Close()
	s.h.Close()
	assert.True(s.T(), s.h.isClosed.Load())
	assert.False(s.T(), s.h.IsReady())

	// Повторный Online не должен работать
	s.h.Online()
	assert.False(s.T(), s.h.IsReady())
}

func (s *PgHndlTestSuite) TestHandleDBError_ContextDeadline() {
	err := s.h.HandleDBError(context.DeadlineExceeded)
	assert.Error(s.T(), err)
	assert.False(s.T(), s.h.IsReady(), "Should go offline on context deadline")
}

func (s *PgHndlTestSuite) TestTx_AlreadyClosedCommit() {
	mTx := &mockTx{commitFn: func() error { return pgx.ErrTxClosed }}
	s.mock.EXPECT().Begin(gomock.Any()).Return(mTx, nil)

	err := s.h.Tx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return nil
	})
	assert.NoError(s.T(), err, "Should ignore ErrTxClosed")
}
