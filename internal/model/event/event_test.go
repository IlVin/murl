package event

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Фиктивный тип для тестов несовпадения типов
type PayloadOther struct {
	Data string `json:"data"`
}

func (PayloadOther) EventType() EvType { return EvUnknown }

func TestEventCycle(t *testing.T) {
	// 1. Создание события
	payload := PayloadAddURL{
		ShardID: 1,
		ID:      100,
		URL:     "https://google.com",
	}

	e, err := MakeEvent(payload)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, e.GetID())
	assert.Equal(t, EvAddURL, e.GetType())

	// 2. Сериализация
	data, err := e.Serialize()
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// 3. Парсинг обратно
	e2, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, e.GetID(), e2.GetID())
	assert.Equal(t, e.GetType(), e2.GetType())

	// 4. Получение Payload
	var result PayloadAddURL
	err = e2.GetPayload(&result)
	require.NoError(t, err)
	assert.Equal(t, payload, result)
}

func TestGetPayload_TypeMismatch(t *testing.T) {
	payload := PayloadAddURL{ID: 1}
	e, _ := MakeEvent(payload)

	// Пытаемся развернуть AddURL в PayloadOther
	var wrong PayloadOther
	err := e.GetPayload(&wrong)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "type mismatch")
}

func TestGetPayload_Empty(t *testing.T) {
	e := &pvtEvent{
		evPayload: nil,
	}
	var res PayloadAddURL
	err := e.GetPayload(&res)
	require.Error(t, err)
	assert.Equal(t, "payload is nil", err.Error())
}

func TestParse_Error(t *testing.T) {
	_, err := Parse([]byte("invalid json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot deserialize")
}

func TestPayloadAddURL_Type(t *testing.T) {
	p := PayloadAddURL{}
	assert.Equal(t, EvAddURL, p.EventType())
}
