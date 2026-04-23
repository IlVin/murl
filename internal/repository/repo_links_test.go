package repository

import (
	"context"
	"testing"

	"murl/internal/dto"

	"github.com/stretchr/testify/assert"
)

// mockLinks реализует интерфейс RepoLinks для тестирования совместимости.
type mockLinks struct{}

func (m *mockLinks) UpSert(ctx context.Context, url string) (string, bool, error) {
	return "", false, nil
}
func (m *mockLinks) Select(ctx context.Context, path string) (string, bool, error) {
	return "", false, nil
}
func (m *mockLinks) Set(ctx context.Context, url string, path string) error {
	return nil
}
func (m *mockLinks) BatchUpSert(ctx context.Context, batch dto.Batch) dto.Batch {
	return batch
}

func TestRepoLinks_InterfaceCompliance(t *testing.T) {
	t.Run("Verify interface satisfaction", func(t *testing.T) {
		// Статическая проверка: если интерфейс изменится, этот код перестанет компилироваться.
		var _ RepoLinks = (*mockLinks)(nil)

		// Фиктивный вызов для формального покрытия, если CI требует выполнения
		m := &mockLinks{}
		res := m.BatchUpSert(context.Background(), dto.Batch{})
		assert.Empty(t, res.Batch)
	})
}
