package event

import (
	"testing"

	uuid "github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestMakeAndGetPayload(t *testing.T) {
	// Данные для теста
	payload := PayloadAddURL{
		OriginalURL: "https://google.com",
		ShortURL:    "http://short/1",
	}

	// 1. Тестируем создание события
	ev, err := MakeEvent(payload, nil)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, ev.GetID())
	assert.Equal(t, EvAddURL, ev.GetType())

	// 2. Тестируем извлечение Payload (Generic)
	extracted, err := GetPayload[PayloadAddURL](ev)
	require.NoError(t, err)
	assert.Equal(t, payload.OriginalURL, extracted.OriginalURL)
}

func TestEventChain(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Создаем родительское событие
	parentID := uuid.New()
	mockParent := NewMockEvent(ctrl)
	mockParent.EXPECT().GetID().Return(parentID).AnyTimes()
	mockParent.EXPECT().GetParents().Return([]uuid.UUID{}).AnyTimes()

	// Создаем дочернее событие через MakeEvent
	payload := PayloadGetURL{ShortURL: "short"}
	childEv, err := MakeEvent(payload, mockParent)

	require.NoError(t, err)
	assert.Contains(t, childEv.GetParents(), parentID)
}

func TestSerialization(t *testing.T) {
	payload := PayloadAddURLBySessionID{
		OriginalURL: "https://yandex.ru",
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
	assert.Equal(t, ev.RawPayload(), parsedEv.RawPayload())
}

func TestTypeMismatch(t *testing.T) {
	payload := PayloadAddURL{OriginalURL: "url"}
	ev, _ := MakeEvent(payload, nil)

	// Пытаемся достать неправильный тип payload
	_, err := GetPayload[PayloadBatch](ev)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "type mismatch")
}
