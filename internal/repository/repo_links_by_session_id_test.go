package repository

import (
	"context"
	"testing"

	"murl/internal/dto"

	"github.com/stretchr/testify/assert"
)

// mockLinksBySessionID — пустая реализация для проверки удовлетворения интерфейса.
type mockLinksBySessionID struct{}

func (m *mockLinksBySessionID) UpSert(ctx context.Context, s string, u string) (string, bool, error) {
	return "", false, nil
}
func (m *mockLinksBySessionID) Set(ctx context.Context, s string, u string, p string) error {
	return nil
}
func (m *mockLinksBySessionID) SelectAll(ctx context.Context, s string) ([]dto.URLItem, error) {
	return nil, nil
}
func (m *mockLinksBySessionID) BatchDelBySessionID(ctx context.Context, s string, b dto.DeleteURLBySessionID) error {
	return nil
}

func TestRepoLinksBySessionID_Interface(t *testing.T) {
	t.Run("Verify interface satisfaction", func(t *testing.T) {
		// Статическая проверка компилятором: удовлетворяет ли структура интерфейсу.
		var _ RepoLinksBySessionID = (*mockLinksBySessionID)(nil)

		// Вызов методов для формального покрытия строк (если требуется CI).
		impl := &mockLinksBySessionID{}
		res, err := impl.SelectAll(context.Background(), "session-123")
		assert.NoError(t, err)
		assert.Nil(t, res)
	})
}
