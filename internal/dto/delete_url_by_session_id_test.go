package dto

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDeleteURLBySessionID_EventType(t *testing.T) {
	t.Run("Check correct event type", func(t *testing.T) {
		d := DeleteURLBySessionID{}
		assert.Equal(t, EvType("EvDeleteURLBySessionID"), d.EventType())
	})

	t.Run("Check data consistency", func(t *testing.T) {
		id := uuid.New()
		urls := []string{"abc", "def", "ghi"}

		d := DeleteURLBySessionID{
			SessionID: id,
			ShortURLs: urls,
		}

		assert.Equal(t, id, d.SessionID)
		assert.Len(t, d.ShortURLs, 3)
		assert.Contains(t, d.ShortURLs, "def")
	})
}
