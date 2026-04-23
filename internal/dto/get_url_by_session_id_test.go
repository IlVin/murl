package dto

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestGetURLBySessionID(t *testing.T) {
	t.Run("Check event type", func(t *testing.T) {
		d := GetURLBySessionID{}
		assert.Equal(t, EvGetURLBySessionID, d.EventType())
	})

	t.Run("Data integrity and JSON tags", func(t *testing.T) {
		id := uuid.New()
		items := []URLItem{
			{ShortURL: "http://short/1", OriginalURL: "http://long/1"},
			{ShortURL: "http://short/2", OriginalURL: "http://long/2"},
		}

		d := GetURLBySessionID{
			SessionID: id,
			Result:    items,
		}

		assert.Equal(t, id, d.SessionID)
		assert.Len(t, d.Result, 2)
		assert.Equal(t, "http://short/1", d.Result[0].ShortURL)
	})
}
