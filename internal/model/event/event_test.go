package event_test

import (
	"testing"

	uuid "github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"murl/internal/mocks"
	"murl/internal/model/event"
)

func TestMakeAndGetPayload(t *testing.T) {
	// Данные для теста
	payload := event.PayloadAddURL{
		OriginalURL: "https://google.com",
		ShortURL:    "http://short/1",
	}

	// 1. Тестируем создание события
	ev, err := event.MakeEvent(payload, nil)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, ev.GetID())
	assert.Equal(t, event.EvAddURL, ev.GetType())

	// 2. Тестируем извлечение Payload (Generic)
	extracted, err := event.GetPayload[event.PayloadAddURL](ev)
	require.NoError(t, err)
	assert.Equal(t, payload.OriginalURL, extracted.OriginalURL)
}

func TestEventChain(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Создаем родительское событие
	parentID := uuid.New()
	mockParent := mocks.NewMockEvent(ctrl)
	mockParent.EXPECT().GetID().Return(parentID).AnyTimes()
	mockParent.EXPECT().GetParents().Return([]uuid.UUID{}).AnyTimes()

	// Создаем дочернее событие через MakeEvent
	payload := event.PayloadGetURL{ShortURL: "short"}
	childEv, err := event.MakeEvent(payload, mockParent)

	require.NoError(t, err)
	assert.Contains(t, childEv.GetParents(), parentID)
}

func TestSerialization(t *testing.T) {
	payload := event.PayloadAddURLBySessionID{
		OriginalURL: "https://yandex.ru",
		SessionID:   uuid.New(),
	}

	ev, _ := event.MakeEvent(payload, nil)

	// Сериализация
	data, err := ev.Serialize()
	require.NoError(t, err)

	// Десериализация (Parse)
	parsedEv, err := event.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, ev.GetID(), parsedEv.GetID())
	assert.Equal(t, ev.GetType(), parsedEv.GetType())
	assert.Equal(t, ev.RawPayload(), parsedEv.RawPayload())
}

func TestTypeMismatch(t *testing.T) {
	payload := event.PayloadAddURL{OriginalURL: "url"}
	ev, _ := event.MakeEvent(payload, nil)

	// Пытаемся достать неправильный тип payload
	_, err := event.GetPayload[event.PayloadBatch](ev)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "type mismatch")
}
