package dto

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestAddURLBySessionID_EventType(t *testing.T) {
	t.Run("Check event type", func(t *testing.T) {
		dto := AddURLBySessionID{}
		assert.Equal(t, EvAddURLBySessionID, dto.EventType())
	})

	t.Run("Data integrity", func(t *testing.T) {
		id := uuid.New()
		original := "https://example.com"

		dto := AddURLBySessionID{
			AddURL: AddURL{
				OriginalURL: original,
			},
			SessionID: id,
		}

		assert.Equal(t, id, dto.SessionID)
		assert.Equal(t, original, dto.OriginalURL)
	})
}
