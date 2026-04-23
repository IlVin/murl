package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchItem_JSON(t *testing.T) {
	t.Run("Check JSON marshaling tags", func(t *testing.T) {
		item := BatchItem{
			CorrelationID: "task-123",
			ShortURL:      "http://short/1",
			ConflictFlag:  true, // Должен быть проигнорирован
		}

		data, err := json.Marshal(item)
		require.NoError(t, err)

		jsonStr := string(data)
		assert.Contains(t, jsonStr, `"correlation_id":"task-123"`)
		assert.Contains(t, jsonStr, `"short_url":"http://short/1"`)

		// Проверяем, что скрытые/пустые поля отсутствуют
		assert.NotContains(t, jsonStr, "conflict_flag")
		assert.NotContains(t, jsonStr, "original_url")
		assert.NotContains(t, jsonStr, "err")
	})

	t.Run("Check Error field in JSON", func(t *testing.T) {
		item := BatchItem{
			CorrelationID: "task-456",
			Err:           "invalid url format",
		}

		data, err := json.Marshal(item)
		require.NoError(t, err)

		assert.Contains(t, string(data), `"err":"invalid url format"`)
	})
}
