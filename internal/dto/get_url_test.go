package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetURL_EventType(t *testing.T) {
	t.Run("Check event type", func(t *testing.T) {
		d := GetURL{}
		assert.Equal(t, EvGetURL, d.EventType())
	})

	t.Run("Check JSON marshaling with IsGone", func(t *testing.T) {
		// Ссылка удалена
		d := GetURL{
			ShortURL: "deleted_link",
			IsGone:   true,
		}

		data, err := json.Marshal(d)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"is_gone":true`)
	})

	t.Run("Check JSON marshaling without IsGone", func(t *testing.T) {
		// Обычная рабочая ссылка
		d := GetURL{
			OriginalURL: "https://example.com",
			ShortURL:    "abc",
			IsGone:      false,
		}

		data, err := json.Marshal(d)
		require.NoError(t, err)

		// Благодаря omitempty поле is_gone должно отсутствовать при значении false
		assert.NotContains(t, string(data), "is_gone")
	})
}
