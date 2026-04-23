// Package event реализует систему типизированных событий для Event Sourcing и аудита.
// Позволяет упаковывать DTO в универсальный конверт с поддержкой прослеживаемости (parent ID).
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"murl/internal/dto"

	uuid "github.com/google/uuid"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=event_mock_test.go -package=$GOPACKAGE

// Event описывает интерфейс конверта события.
// Конверт содержит метаданные (ID, тип, связи) и саму полезную нагрузку (payload).
type Event interface {
	// GetID возвращает уникальный идентификатор конкретного экземпляра события.
	GetID() uuid.UUID
	// GetType возвращает тип события из справочника dto.EvType.
	GetType() dto.EvType
	// GetParents возвращает список ID всех предшествующих событий в цепочке.
	GetParents() []uuid.UUID
	// Serialize преобразует событие вместе с метаданными в JSON-байт-массив.
	Serialize() ([]byte, error)
	// RawPayload возвращает сырые данные полезной нагрузки (JSON).
	RawPayload() []byte
}

// Payload определяет ограничение (constraint) для типов, которые могут быть упакованы в событие.
// Сюда должны входить все DTO, реализующие метод EventType().
type Payload interface {
	dto.AddURL | dto.AddURLBySessionID |
		dto.GetURL | dto.GetURLBySessionID |
		dto.Batch | dto.BatchBySessionID |
		dto.DeleteURLBySessionID

	EventType() dto.EvType
}

// MakeEvent — универсальный конструктор события.
// Принимает полезную нагрузку и опциональное родительское событие для построения цепочки.
// Автоматически генерирует новый UUID для события и наследует историю родителей.
func MakeEvent[T Payload](payload T, parentEvent Event) (Event, error) {
	pData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize: %w", err)
	}

	evType := any(payload).(interface{ EventType() dto.EvType }).EventType()

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

// GetPayload — типизированный хелпер для извлечения данных из конверта.
// Выполняет проверку соответствия типа события целевой структуре.
func GetPayload[T Payload](e Event) (T, error) {
	var dest T

	// Аналогично достаем тип для проверки
	targetType := any(dest).(interface{ EventType() dto.EvType }).EventType()

	if e.GetType() != targetType {
		return dest, fmt.Errorf("type mismatch: event has %v, target is %v", e.GetType(), targetType)
	}

	if err := json.Unmarshal(e.RawPayload(), &dest); err != nil {
		return dest, fmt.Errorf("unmarshal failed: %w", err)
	}

	return dest, nil
}

// Parse выполняет десериализацию байтового потока в объект Event.
// Используется при чтении событий из внешних хранилищ или очередей.
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

// baseEvent — внутренняя реализация интерфейса Event.
type baseEvent struct {
	evParentID []uuid.UUID
	evID       uuid.UUID
	evType     dto.EvType
	evPayload  json.RawMessage
}

// GetID возвращает уникальный идентификатор события. Реализует интерфейс Event.
func (e *baseEvent) GetID() uuid.UUID { return e.evID }

// GetType возвращает константный тип события из пакета dto. Реализует интерфейс Event.
func (e *baseEvent) GetType() dto.EvType { return e.evType }

// RawPayload возвращает полезную нагрузку события в формате JSON (байты). Реализует интерфейс Event.
func (e *baseEvent) RawPayload() []byte { return e.evPayload }

// GetParents возвращает срез идентификаторов всех родительских событий.
// Метод выполняет копирование данных (append в nil), чтобы гарантировать иммутабельность
// внутреннего состояния события при изменении среза вызывающей стороной.
func (e *baseEvent) GetParents() []uuid.UUID {
	return append([]uuid.UUID(nil), e.evParentID...)
}

// serializeEnvelope — внутренняя структура для обеспечения стабильного формата JSON.
type serializeEnvelope struct {
	ParentID []uuid.UUID     `json:"parent_id,omitempty"`
	ID       uuid.UUID       `json:"id"`
	Type     dto.EvType      `json:"type"`
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
