package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"

	uuid "github.com/google/uuid"
)

var (
	ErrEvPayloadNotImpl     error = errors.New("payload for EvType not implemented")
	ErrEvPayloadUndef       error = errors.New("payload undefined")
	ErrEvNotCompPayloadType error = errors.New("not compatible payload type")
)

// getEvTypeByReflect возвращает константу EvType для переданного типа Go.
func getEvTypeByReflect(t reflect.Type) (EvType, error) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t {
	case reflect.TypeOf(PayloadAddURL{}):
		return EvAddURL, nil
	default:
		return EvUnknown, ErrEvPayloadNotImpl
	}
}

// getReflectByEvType возвращает reflect.Type (тип структуры payload) на основе константы EvType.
func getReflectByEvType(t EvType) (reflect.Type, error) {
	switch t {
	case EvAddURL:
		return reflect.TypeOf(&PayloadAddURL{}), nil
	default:
		return nil, ErrEvPayloadNotImpl
	}
}

// =========  Фабрика IEvent  =========
func NewEvent[T any]() (IEvent, *T, error) {
	// 1. Получаем reflect.Type для дженерика T без аллокаций
	tType := reflect.TypeOf((*T)(nil)).Elem()

	// 2. Определяем EvType через отдельную функцию
	evType, err := getEvTypeByReflect(tType)
	if err != nil {
		return nil, nil, err
	}

	// 3. Создаем объект данных (возвращаем указатель *T)
	payload := new(T)

	// 4. Создаем обертку события c пустым evPayload
	event := &pvtTEvent{
		evID:      uuid.New(),
		evType:    evType,
		evPayload: nil,
	}

	return event, payload, nil
}

// =========  Типы событий  =========
type EvType uint32

const (
	EvUnknown EvType = iota
	EvAddURL
)

// ---------  Свой Тип Payload для каждого EvType  ---------

type PayloadAny struct{}

type PayloadAddURL struct {
	ShardID byte   `json:"shard_id"`
	ID      uint64 `json:"id"`
	URL     string `json:"url"`
}

// =========  Интерфейс IEvent  =========
// Разнотипные события храним в одно слайсе типа IEvent

type IEvent interface {
	GetID() uuid.UUID
	GetType() EvType
	Serialize() ([]byte, error)
	GetPayload(dest any) error
	SetPayload(src any) error
}

// =========  Базовая реализация (pvtTEvent)  =========

type pvtTEvent struct {
	mu        sync.RWMutex
	evID      uuid.UUID
	evType    EvType
	evPayload json.RawMessage
}

func (e *pvtTEvent) GetID() uuid.UUID {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.evID
}

func (e *pvtTEvent) GetType() EvType {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.evType
}

type serializeEnvelope struct {
	ID      uuid.UUID       `json:"id"`
	Type    EvType          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (e *pvtTEvent) Serialize() ([]byte, error) {
	e.mu.RLock()
	env := serializeEnvelope{
		ID:      e.evID,
		Type:    e.evType,
		Payload: e.evPayload,
	}
	e.mu.RUnlock()
	return json.Marshal(&env)
}

func Parse(data []byte) (IEvent, error) {
	env := serializeEnvelope{}

	err := json.Unmarshal(data, &env)
	if err != nil {
		return nil, fmt.Errorf("cannot deserialize: %w", err)
	}

	return &pvtTEvent{
		evID:      env.ID,
		evType:    env.Type,
		evPayload: env.Payload,
	}, nil
}

func (e *pvtTEvent) GetPayload(dest any) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.evPayload == nil {
		return ErrEvPayloadUndef
	}
	return json.Unmarshal(e.evPayload, dest)
}

func (e *pvtTEvent) SetPayload(src any) error {
	if src == nil {
		e.mu.Lock()
		e.evPayload = nil
		e.mu.Unlock()
		return nil
	}
	rType, err := getReflectByEvType(e.GetType())
	if err != nil {
		return fmt.Errorf("cannot set payload: %w", err)
	}
	sType := reflect.TypeOf(src)
	if sType != rType {
		return ErrEvNotCompPayloadType
	}

	data, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("cannot serialize payload: %w", err)
	}
	e.mu.Lock()
	e.evPayload = data
	e.mu.Unlock()
	return nil
}
