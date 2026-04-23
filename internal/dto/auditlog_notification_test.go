package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditlogNotification(t *testing.T) {
	t.Run("JSON Marshaling with UserID", func(t *testing.T) {
		id := uuid.New()
		now := time.Now().Unix()

		notif := AuditlogNotification{
			UnixTimestamp: now,
			Action:        "shorten",
			UserID:        &id,
			OrigURL:       "https://example.com",
		}

		data, err := json.Marshal(notif)
		require.NoError(t, err)

		assert.Contains(t, string(data), id.String())
		assert.Contains(t, string(data), `"action":"shorten"`)
	})

	t.Run("JSON Marshaling without UserID (omitempty)", func(t *testing.T) {
		notif := AuditlogNotification{
			UnixTimestamp: 123456789,
			Action:        "follow",
			UserID:        nil,
			OrigURL:       "https://example.com",
		}

		data, err := json.Marshal(notif)
		require.NoError(t, err)

		assert.NotContains(t, string(data), "user_id")
	})
}
