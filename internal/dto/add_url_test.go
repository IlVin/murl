package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddURL_EventType(t *testing.T) {
	t.Run("Check correct event type mapping", func(t *testing.T) {
		d := AddURL{}
		assert.Equal(t, EvAddURL, d.EventType(), "AddURL must return EvAddURL type")
	})

	t.Run("Field integrity", func(t *testing.T) {
		original := "https://google.com"
		short := "http://localhost:8080/abcd"

		d := AddURL{
			OriginalURL:  original,
			ShortURL:     short,
			ConflictFlag: true,
		}

		assert.Equal(t, original, d.OriginalURL)
		assert.Equal(t, short, d.ShortURL)
		assert.True(t, d.ConflictFlag)
	})
}
