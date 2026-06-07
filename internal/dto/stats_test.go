package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStats_JSON(t *testing.T) {
	t.Run("marshal success with data", func(t *testing.T) {
		s := Stats{
			URLs:  150,
			Users: 42,
		}

		data, err := json.Marshal(s)
		require.NoError(t, err)

		expectedJSON := `{"urls":150,"users":42}`
		assert.JSONEq(t, expectedJSON, string(data))
	})

	t.Run("marshal success with omitempty", func(t *testing.T) {
		s := Stats{}

		data, err := json.Marshal(s)
		require.NoError(t, err)

		// Из-за тегов omitempty пустые поля uint64 (0) не должны попасть в JSON
		expectedJSON := `{}`
		assert.JSONEq(t, expectedJSON, string(data))
	})

	t.Run("unmarshal success", func(t *testing.T) {
		inputJSON := `{"urls":1000,"users":500}`

		var s Stats
		err := json.Unmarshal([]byte(inputJSON), &s)
		require.NoError(t, err)

		assert.Equal(t, uint64(1000), s.URLs)
		assert.Equal(t, uint64(500), s.Users)
	})
}
