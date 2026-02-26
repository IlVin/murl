package event

import (
	"testing"

	uuid "github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeEvent_And_GetPayload(t *testing.T) {
	// 1. Успешный цикл: Создание -> Получение данных
	t.Run("success_lifecycle", func(t *testing.T) {
		payload := PayloadAddURL{
			OriginalURL: "https://google.com",
			ShortURL:    "http://short/1",
		}

		ev, err := MakeEvent(payload, nil)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, ev.GetID())
		assert.Equal(t, EvAddURL, ev.GetType())

		var result PayloadAddURL
		err = ev.GetPayload(&result)
		require.NoError(t, err)
		assert.Equal(t, payload.OriginalURL, result.OriginalURL)
	})

	// 2. Ошибка при несоответствии типов
	t.Run("type_mismatch", func(t *testing.T) {
		payload := PayloadAddURL{OriginalURL: "test"}
		ev, _ := MakeEvent(payload, nil)

		var wrongPayload PayloadGetURL
		err := ev.GetPayload(&wrongPayload)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type mismatch")
	})

	// 3. Передача nil в GetPayload
	t.Run("nil_dest", func(t *testing.T) {
		payload := PayloadAddURL{OriginalURL: "test"}
		ev, _ := MakeEvent(payload, nil)

		err := ev.GetPayload(nil)
		assert.Error(t, err)
		assert.Equal(t, "dest is nil", err.Error())
	})
}

func TestEvent_Hierarchy(t *testing.T) {
	// Создаем цепочку: Grandparent -> Parent -> Child
	gp, _ := MakeEvent(PayloadAddURL{OriginalURL: "gp"}, nil)
	p, _ := MakeEvent(PayloadAddURL{OriginalURL: "p"}, gp)
	c, _ := MakeEvent(PayloadAddURL{OriginalURL: "c"}, p)

	parents := c.GetParents()
	require.Equal(t, 2, len(parents))
	assert.Equal(t, gp.GetID(), parents[0])
	assert.Equal(t, p.GetID(), parents[1])

	// Проверка иммутабельности (копирования слайса)
	parents[0] = uuid.New()
	assert.NotEqual(t, c.GetParents()[0], parents[0], "GetParents should return a copy")
}

func TestEvent_Serialization(t *testing.T) {
	payload := PayloadAddURLBySessionID{
		OriginalURL: "https://ya.ru",
		SessionID:   uuid.New(),
	}
	ev, _ := MakeEvent(payload, nil)

	// Сериализация
	data, err := ev.Serialize()
	require.NoError(t, err)

	// Десериализация (Parse)
	parsedEv, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, ev.GetID(), parsedEv.GetID())
	assert.Equal(t, ev.GetType(), parsedEv.GetType())

	// Проверка Payload после восстановления
	var restoredPayload PayloadAddURLBySessionID
	err = parsedEv.GetPayload(&restoredPayload)
	require.NoError(t, err)
	assert.Equal(t, payload.SessionID, restoredPayload.SessionID)
}

func TestParse_Errors(t *testing.T) {
	t.Run("invalid_json", func(t *testing.T) {
		_, err := Parse([]byte("{invalid}"))
		assert.Error(t, err)
	})

	t.Run("missing_id", func(t *testing.T) {
		badData := []byte(`{"type": 1, "payload": {}}`)
		_, err := Parse(badData)
		assert.Error(t, err)
		assert.Equal(t, "event ID is missing", err.Error())
	})
}

func TestBatchPayloads(t *testing.T) {
	// Проверка корректности работы с новыми структурами Batch
	payload := PayloadBatchBySessionID{
		SessionID: uuid.New(),
		Batch: []struct {
			CorrelationID string `json:"correlation_id"`
			OriginalURL   string `json:"original_url,omitempty"`
			ShortURL      string `json:"short_url,omitempty"`
			ConflictFlag  bool   `json:"-"`
			Err           string `json:"err,omitempty"`
		}{
			{CorrelationID: "1", OriginalURL: "url1"},
		},
	}

	ev, err := MakeEvent(payload, nil)
	require.NoError(t, err)

	var result PayloadBatchBySessionID
	err = ev.GetPayload(&result)
	require.NoError(t, err)
	assert.Equal(t, 1, len(result.Batch))
	assert.Equal(t, "url1", result.Batch[0].OriginalURL)
}
