package event

import (
	"encoding/json"
	"fmt"

	uuid "github.com/google/uuid"
)

type EvType uint32

// Возможные типы Event.
const (
	EvUnknown EvType = iota
	EvAddURL
	// Добавляем сюда новый тип
	// и создаем к нему соответствующий тип Payload
)

// Интерфейс события
type Event interface {
	GetID() uuid.UUID
	GetType() EvType
	Serialize() ([]byte, error)
	GetPayload(dest any) error
}

// ============  Типы Payload  ============
type Payload interface {
	EventType() EvType
}

// ------------  EvAddURL  ------------
type PayloadAddURL struct {
	ShardID byte   `json:"shard_id"`
	ID      uint64 `json:"id"`
	URL     string `json:"url"`
}

func (PayloadAddURL) EventType() EvType { return EvAddURL }

//  ------------  /EvAddURL  ------------

//  ------------  EvAddURL  ------------
//  Здесь новый тип Event
//  ------------  /EvAddURL  ------------

// Конструктор Event из Payload
func MakeEvent(payload Payload) (Event, error) {
	pData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize payload (%v): %w", payload, err)
	}
	e := &pvtEvent{
		evID:      uuid.New(),
		evType:    payload.EventType(),
		evPayload: pData,
	}

	return e, nil
}

// Конструктор Event из []byte
func Parse(data []byte) (Event, error) {
	env := serializeEnvelope{}

	err := json.Unmarshal(data, &env)
	if err != nil {
		return nil, fmt.Errorf("cannot deserialize: %w", err)
	}

	return &pvtEvent{
		evID:      env.ID,
		evType:    env.Type,
		evPayload: env.Payload,
	}, nil
}

type pvtEvent struct {
	evID      uuid.UUID
	evType    EvType
	evPayload json.RawMessage
}

func (e *pvtEvent) GetID() uuid.UUID {
	return e.evID
}

func (e *pvtEvent) GetType() EvType {
	return e.evType
}

type serializeEnvelope struct {
	ID      uuid.UUID       `json:"id"`
	Type    EvType          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (e *pvtEvent) Serialize() ([]byte, error) {
	env := serializeEnvelope{
		ID:      e.evID,
		Type:    e.evType,
		Payload: e.evPayload,
	}
	return json.Marshal(&env)
}

func (e *pvtEvent) GetPayload(dest any) error {
	// Такого не может быть, но вдруг, как всегда?!...
	if len(e.evPayload) == 0 {
		return fmt.Errorf("payload is nil")
	}
	if p, ok := dest.(Payload); ok {
		if p.EventType() != e.evType {
			return fmt.Errorf("type mismatch: event has %v, dest has %v", e.evType, p.EventType())
		}
	}
	return json.Unmarshal(e.evPayload, dest)
}
