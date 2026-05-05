package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBatch_EventType(t *testing.T) {
	t.Run("Check correct event type mapping", func(t *testing.T) {
		b := Batch{}
		assert.Equal(t, EvType("EvBatch"), b.EventType(), "Batch must return EvBatch type")
	})

	t.Run("Data consistency", func(t *testing.T) {
		items := []BatchItem{
			{CorrelationID: "a", OriginalURL: "http://site-a.com"},
			{CorrelationID: "b", OriginalURL: "http://site-b.com"},
		}

		b := Batch{
			Batch: items,
		}

		assert.Len(t, b.Batch, 2)
		assert.Equal(t, "a", b.Batch[0].CorrelationID)
		assert.Equal(t, "http://site-b.com", b.Batch[1].OriginalURL)
	})
}
