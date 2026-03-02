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
	EvDeleteURLBySessionID
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
	RawPayload() []byte
}

// ============  Типы Payload  ============
type Payload interface {
	PayloadAddURL | PayloadAddURLBySessionID |
		PayloadGetURL | PayloadGetURLBySessionID |
		PayloadBatch | PayloadBatchBySessionID |
		PayloadDeleteURLBySessionID

	EventType() EvType
}

// !!! [3] СЮДА ДОБАВЬ НОВЫЙ ТИП ПОЛЕЗНОЙ НАГРУЗКИ СОБЫТИЯ !!!

// ------------  EvAddURL  ------------
type PayloadAddURL struct {
	OriginalURL  string `json:"original_url"`
	ShortURL     string `json:"short_url"`
	ConflictFlag bool   `json:"conflict_flag"`
}

func (PayloadAddURL) EventType() EvType { return EvAddURL }

//  ------------  /EvAddURL  ------------

// ------------  EvAddURLBySessionID  ------------
type PayloadAddURLBySessionID struct {
	OriginalURL  string    `json:"original_url"`
	ShortURL     string    `json:"short_url"`
	SessionID    uuid.UUID `json:"session_id"`
	ConflictFlag bool      `json:"conflict_flag"`
}

func (PayloadAddURLBySessionID) EventType() EvType { return EvAddURLBySessionID }

//  ------------  /EvAddURLBySessionID  ------------

// ------------  EvGetURL  ------------
type PayloadGetURL struct {
	OriginalURL string `json:"original_url"`
	ShortURL    string `json:"short_url"`
	IsGone      bool   `json:"is_gone,omitempty"`
}

func (PayloadGetURL) EventType() EvType { return EvGetURL }

//  ------------  /EvGetURL  ------------

// ------------  EvGetURLBySessionID  ------------
type PayloadURLItem struct {
	CorrelationID string `json:"correlation_id,omitempty"`
	OriginalURL   string `json:"original_url,omitempty"`
	ShortURL      string `json:"short_url,omitempty"`
}
type PayloadGetURLBySessionID struct {
	SessionID uuid.UUID        `json:"session_id,omitempty"`
	Result    []PayloadURLItem `json:"result,omitempty"`
}

func (PayloadGetURLBySessionID) EventType() EvType { return EvGetURLBySessionID }

//  ------------  /EvGetURLBySessionID  ------------

// ------------  EvDeleteURLBySessionID  ------------
type PayloadDeleteURLBySessionID struct {
	SessionID uuid.UUID `json:"session_id,omitempty"`
	ShortURLs []string  `json:"short_urls,omitempty"`
}

func (PayloadDeleteURLBySessionID) EventType() EvType { return EvDeleteURLBySessionID }

//  ------------  /EvDeleteURLBySessionID  ------------

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
	SessionID uuid.UUID          `json:"session_id"`
	Batch     []PayloadBatchItem `json:"batch"`
}

func (PayloadBatchBySessionID) EventType() EvType { return EvBatchBySessionID }

//  ------------  /EvBatch  ------------

//  ------------  EvNewType  ------------
//  Здесь новый тип Event
//  ------------  /EvNewType  ------------

// ============  Конструкторы (Generics)  ============
// MakeEvent - Дженерик-конструктор
func MakeEvent[T Payload](payload T, parentEvent Event) (Event, error) {
	pData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize: %w", err)
	}

	evType := any(payload).(interface{ EventType() EvType }).EventType()

	e := &baseEvent{
		evID:      uuid.New(),
		evType:    evType,
		evPayload: pData,
	}

	if parentEvent != nil {
		e.evParentID = append(parentEvent.GetParents(), parentEvent.GetID())
	}

	return e, nil
}

// GetPayload - Дженерик-хелпер для извлечения данных из Event
func GetPayload[T Payload](e Event) (T, error) {
	var dest T

	// Аналогично достаем тип для проверки
	targetType := any(dest).(interface{ EventType() EvType }).EventType()

	if e.GetType() != targetType {
		return dest, fmt.Errorf("type mismatch: event has %v, target is %v", e.GetType(), targetType)
	}

	if err := json.Unmarshal(e.RawPayload(), &dest); err != nil {
		return dest, fmt.Errorf("unmarshal failed: %w", err)
	}

	return dest, nil
}

// Parse - десериализация конверта из []byte
func Parse(data []byte) (Event, error) {
	var env serializeEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
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

// ============  Внутренняя реализация  ============

type baseEvent struct {
	evParentID []uuid.UUID
	evID       uuid.UUID
	evType     EvType
	evPayload  json.RawMessage
}

func (e *baseEvent) GetID() uuid.UUID   { return e.evID }
func (e *baseEvent) GetType() EvType    { return e.evType }
func (e *baseEvent) RawPayload() []byte { return e.evPayload }
func (e *baseEvent) GetParents() []uuid.UUID {
	return append([]uuid.UUID(nil), e.evParentID...)
}

type serializeEnvelope struct {
	ParentID []uuid.UUID     `json:"parent_id,omitempty"`
	ID       uuid.UUID       `json:"id"`
	Type     EvType          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
}

func (e *baseEvent) Serialize() ([]byte, error) {
	return json.Marshal(serializeEnvelope{
		ParentID: e.evParentID,
		ID:       e.evID,
		Type:     e.evType,
		Payload:  e.evPayload,
	})
}
