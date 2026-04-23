package repository

import (
	"context"
	"testing"

	"murl/internal/dto"
	"murl/internal/model/event"

	"github.com/stretchr/testify/assert"
)

// Тестируем глобальные переменные пакета
func TestRepo_Globals(t *testing.T) {
	t.Run("Error string", func(t *testing.T) {
		assert.Equal(t, "internal server error", ErrInternalServerError.Error())
	})
}

// Статическая проверка реализации интерфейса.
// Это не создает исполняемого кода в рантайме, но гарантирует, что
// любая структура, претендующая на роль Repo, будет обязана реализовать все методы.
type mockRepo struct{}

func (m *mockRepo) On(context.Context, event.Event) error                  { return nil }
func (m *mockRepo) Ping(context.Context) error                             { return nil }
func (m *mockRepo) Close(context.Context) error                            { return nil }
func (m *mockRepo) AddURL(context.Context, dto.AddURL) (dto.AddURL, error) { return dto.AddURL{}, nil }
func (m *mockRepo) AddURLBySessionID(context.Context, dto.AddURLBySessionID) (dto.AddURLBySessionID, error) {
	return dto.AddURLBySessionID{}, nil
}
func (m *mockRepo) GetURL(context.Context, dto.GetURL) (dto.GetURL, error) { return dto.GetURL{}, nil }
func (m *mockRepo) GetURLBySessionID(context.Context, dto.GetURLBySessionID) (dto.GetURLBySessionID, error) {
	return dto.GetURLBySessionID{}, nil
}
func (m *mockRepo) DeleteURLBySessionID(context.Context, dto.DeleteURLBySessionID) error { return nil }
func (m *mockRepo) Batch(context.Context, dto.Batch) (dto.Batch, error)                  { return dto.Batch{}, nil }

func TestRepo_InterfaceCompliance(t *testing.T) {
	// Эта проверка подтверждает, что mockRepo соответствует интерфейсу Repo.
	// Помогает отловить ошибки несовместимости на этапе компиляции тестов.
	var _ Repo = (*mockRepo)(nil)
}
