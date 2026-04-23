package dto

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestBatchBySessionID(t *testing.T) {
	t.Run("Check event type mapping", func(t *testing.T) {
		d := BatchBySessionID{}
		assert.Equal(t, EvBatchBySessionID, d.EventType())
	})

	t.Run("Structure integrity", func(t *testing.T) {
		id := uuid.New()
		items := []BatchItem{
			{CorrelationID: "1", OriginalURL: "http://test.com"},
			{CorrelationID: "2", OriginalURL: "http://example.com"},
		}

		d := BatchBySessionID{
			SessionID: id,
			Batch:     items,
		}

		assert.Equal(t, id, d.SessionID)
		assert.Len(t, d.Batch, 2)
		assert.Equal(t, "1", d.Batch[0].CorrelationID)
	})
}
