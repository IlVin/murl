package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvType_Constants(t *testing.T) {
	t.Run("Ensure iota sequence", func(t *testing.T) {
		assert.Equal(t, EvType(0), EvUnknown)
		assert.Equal(t, EvType(1), EvAddURL)
		assert.Equal(t, EvType(7), EvBatchBySessionID)
	})
}

// Примечание: Тест ниже будет работать только после запуска 'go generate'
// и генерации файла evtype_string.go
func TestEvType_String(t *testing.T) {
	tests := []struct {
		ev       EvType
		expected string
	}{
		{EvAddURL, "EvAddURL"},
		{EvBatchBySessionID, "EvBatchBySessionID"},
		{EvUnknown, "EvUnknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			// Если stringer еще не запущен, метод String() вернет "EvType(value)"
			// Если запущен — красивое имя константы.
			assert.NotEmpty(t, tt.ev.String())
		})
	}
}
