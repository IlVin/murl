package event

import (
	"encoding/json"
	"errors"
	"fmt"

	uuid "github.com/google/uuid"
)

type EvType int32

// !!! [3] НЕ ЗАБУДЬ ПЕРЕГЕНЕРИРОВАТЬ !!!
//go:generate $GOPATH/bin/stringer -type=EvType

// !!! [1] СЮДА ДОПИШИ НОВЫЙ ТИП КОНСТАНТЫ !!!
// Возможные типы Event.
const (
	EvUnknown EvType = iota
	EvAddURL
	EvAddURLBySessionID
	EvGetURL
	EvGetURLBySessionID
	EvBatch
	EvBatchBySessionID
	// Добавляем сюда новый тип
	// и создаем к нему соответствующий тип Payload
)

// Интерфейс события
type Event interface {
	GetID() uuid.UUID
	GetType() EvType
	GetParents() []uuid.UUID
	Serialize() ([]byte, error)
	GetPayload(dest any) error
}

// ============  Типы Payload  ============
type Payload interface {
	EventType() EvType
}

// !!! [3] СЮДА ДОБАВЬ НОВЫЙ ТИП ПОЛЕЗНОЙ НАГРУЗКИ СОБЫТИЯ !!!

// ------------  EvAddURL  ------------
type PayloadAddURL struct {
	OriginalURL  string `json:"original_url"`
	ShortURL     string `json:"short_url"`
	ConflictFlag bool   `json:"-"`
}

func (PayloadAddURL) EventType() EvType { return EvAddURL }

//  ------------  /EvAddURL  ------------

// ------------  EvAddURLBySessionID  ------------
type PayloadAddURLBySessionID struct {
	OriginalURL  string    `json:"original_url"`
	ShortURL     string    `json:"short_url"`
	SessionID    uuid.UUID `json:"session_id"`
	ConflictFlag bool      `json:"-"`
}

func (PayloadAddURLBySessionID) EventType() EvType { return EvAddURLBySessionID }

//  ------------  /EvAddURLBySessionID  ------------

// ------------  EvGetURL  ------------
type PayloadGetURL struct {
	OriginalURL  string `json:"original_url"`
	ShortURL     string `json:"short_url"`
	ConflictFlag bool   `json:"-"`
}

func (PayloadGetURL) EventType() EvType { return EvGetURL }

//  ------------  /EvGetURL  ------------

// ------------  EvGetURLBySessionID  ------------
type PayloadGetURLBySessionID struct {
	SessionID uuid.UUID `json:"session_id"`
	Result    []struct {
		OriginalURL string `json:"original_url"`
		ShortURL    string `json:"short_url"`
	} `json:"result"`
}

func (PayloadGetURLBySessionID) EventType() EvType { return EvGetURLBySessionID }

//  ------------  /EvGetURLBySessionID  ------------

// ------------  EvBatch  ------------
type PayloadBatchItem struct {
	CorrelationID string `json:"correlation_id"`
	OriginalURL   string `json:"original_url,omitempty"`
	ShortURL      string `json:"short_url,omitempty"`
	ConflictFlag  bool   `json:"-"`
	Err           string `json:"err,omitempty"`
}
type PayloadBatch struct {
	Batch []PayloadBatchItem `json:"batch"`
}

func (PayloadBatch) EventType() EvType { return EvBatch }

//  ------------  /EvBatch  ------------

// ------------  EvBatchBySessionID  ------------
type PayloadBatchBySessionID struct {
	SessionID uuid.UUID `json:"session_id"`
	Batch     []struct {
		CorrelationID string `json:"correlation_id"`
		OriginalURL   string `json:"original_url,omitempty"`
		ShortURL      string `json:"short_url,omitempty"`
		ConflictFlag  bool   `json:"-"`
		Err           string `json:"err,omitempty"`
	} `json:"batch"`
}

func (PayloadBatchBySessionID) EventType() EvType { return EvBatchBySessionID }

//  ------------  /EvBatch  ------------

//  ------------  EvNewType  ------------
//  Здесь новый тип Event
//  ------------  /EvNewType  ------------

// Конструктор Event из Payload
func MakeEvent(payload Payload, parentEvent Event) (Event, error) {
	pData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize payload (%v): %w", payload, err)
	}
	e := &baseEvent{
		evID:      uuid.New(),
		evType:    payload.EventType(),
		evPayload: pData,
	}

	if parentEvent != nil {
		e.evParentID = append(parentEvent.GetParents(), parentEvent.GetID())
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

	if env.ID == uuid.Nil {
		return nil, errors.New("event ID is missing")
	}

	return &baseEvent{
		evParentID: env.ParentID,
		evID:       env.ID,
		evType:     env.Type,
		evPayload:  env.Payload,
	}, nil
}

type baseEvent struct {
	evParentID []uuid.UUID
	evID       uuid.UUID
	evType     EvType
	evPayload  json.RawMessage
}

func (e *baseEvent) GetID() uuid.UUID {
	return e.evID
}

func (e *baseEvent) GetType() EvType {
	return e.evType
}

func (e *baseEvent) GetParents() []uuid.UUID {
	cp := make([]uuid.UUID, len(e.evParentID))
	copy(cp, e.evParentID)
	return cp
}

type serializeEnvelope struct {
	ParentID []uuid.UUID     `json:"parent_id,omitempty"`
	ID       uuid.UUID       `json:"id"`
	Type     EvType          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
}

func (e *baseEvent) Serialize() ([]byte, error) {
	env := serializeEnvelope{
		ParentID: e.evParentID,
		ID:       e.evID,
		Type:     e.evType,
		Payload:  e.evPayload,
	}
	return json.Marshal(&env)
}

func (e *baseEvent) GetPayload(dest any) error {
	if dest == nil {
		return errors.New("dest is nil")
	}

	if len(e.evPayload) == 0 {
		return errors.New("payload is nil")
	}

	p, ok := dest.(Payload)
	if !ok {
		return errors.New("type mismatch: dest is not Payload type")
	}

	if p.EventType() != e.evType {
		return fmt.Errorf("type mismatch: event has %v, dest has %v", e.evType, p.EventType())
	}

	return json.Unmarshal(e.evPayload, dest)
}
