package model

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// 1. Тестирование маппинга типов
func TestTypeMapping(t *testing.T) {
	t.Run("GetEvTypeByReflect", func(t *testing.T) {
		// Успешный кейс (структура и указатель)
		{
			typ, err := getEvTypeByReflect(reflect.TypeOf(PayloadAddURL{}))
			assert.NoError(t, err)
			assert.Equal(t, EvAddURL, typ)
		}

		// Успешный кейс (структура и указатель)
		{
			typ, err := getEvTypeByReflect(reflect.TypeOf(&PayloadAddURL{}))
			assert.NoError(t, err)
			assert.Equal(t, EvAddURL, typ)
		}

		// Ошибка: неизвестный тип
		{
			typ, err := getEvTypeByReflect(reflect.TypeOf(struct{}{}))
			assert.ErrorIs(t, err, ErrEvPayloadNotImpl)
			assert.Equal(t, EvUnknown, typ)
		}
	})

	t.Run("GetReflectByEvType", func(t *testing.T) {
		// Успешный кейс
		{
			typ, err := getReflectByEvType(EvAddURL)
			assert.NoError(t, err)
			assert.Equal(t, reflect.TypeOf(&PayloadAddURL{}), typ)
		}

		// Ошибка: неизвестный тип
		{
			typ, err := getReflectByEvType(EvUnknown)
			assert.Nil(t, typ)
			assert.ErrorIs(t, err, ErrEvPayloadNotImpl)
		}
	})
}

// 2. Тестирование фабрик
func TestFactories(t *testing.T) {
	t.Run("NewEvent Success", func(t *testing.T) {
		ev, payload, err := NewEvent[PayloadAddURL]()
		assert.NoError(t, err)
		assert.NotNil(t, ev)
		assert.NotNil(t, payload)
		assert.Equal(t, EvAddURL, ev.GetType())
		assert.NotEqual(t, uuid.Nil, ev.GetID())
		assert.IsType(t, &PayloadAddURL{}, payload)
	})

	t.Run("NewEvent Failure (Unsupported Type)", func(t *testing.T) {
		ev, payload, err := NewEvent[struct{}]()
		assert.ErrorIs(t, err, ErrEvPayloadNotImpl)
		assert.Nil(t, ev)
		assert.Nil(t, payload)
	})

}

// 3. Тестирование методов TEvent (Set/Get Payload)
func TestTEvent_PayloadHandling(t *testing.T) {
	ev, _, _ := NewEvent[PayloadAddURL]()

	t.Run("Set and Get Success", func(t *testing.T) {
		data := &PayloadAddURL{URL: "test.com", ID: 123}
		err := ev.SetPayload(data)
		assert.NoError(t, err)

		var result PayloadAddURL
		err = ev.GetPayload(&result)
		assert.NoError(t, err)
		assert.Equal(t, data.URL, result.URL)
	})

	t.Run("Get Undefined Payload", func(t *testing.T) {
		newEv := &pvtTEvent{evType: EvAddURL} // evPayload is nil
		var result PayloadAddURL
		err := newEv.GetPayload(&result)
		assert.ErrorIs(t, err, ErrEvPayloadUndef)
	})

	t.Run("Set Nil Payload", func(t *testing.T) {
		err := ev.SetPayload(nil)
		assert.NoError(t, err)
		err = ev.GetPayload(&PayloadAddURL{})
		assert.ErrorIs(t, err, ErrEvPayloadUndef)
	})

	t.Run("Set Incompatible Type", func(t *testing.T) {
		wrongData := struct{ Name string }{Name: "Wrong"}
		err := ev.SetPayload(&wrongData)
		assert.ErrorIs(t, err, ErrEvNotCompPayloadType)
	})

	t.Run("Set Type with Error (Mock implementation issue)", func(t *testing.T) {
		// Создаем событие с невалидным типом вручную для покрытия ошибки в SetPayload
		brokenEv := &pvtTEvent{evType: 999}
		err := brokenEv.SetPayload(&PayloadAddURL{})
		assert.Contains(t, err.Error(), "cannot set payload")
	})
}

// 4. Тестирование сериализации и Parse
func TestSerialization(t *testing.T) {
	ev, p, _ := NewEvent[PayloadAddURL]()
	p.URL = "http://io.io/"
	ev.SetPayload(p)

	t.Run("Serialize and Parse Success", func(t *testing.T) {
		data, err := ev.Serialize()
		fmt.Printf("DATA: %v", string(data))
		assert.NoError(t, err)
		assert.NotEmpty(t, data)

		parsed, err := Parse(data)
		assert.NoError(t, err)
		assert.Equal(t, ev.GetID(), parsed.GetID())
		assert.Equal(t, ev.GetType(), parsed.GetType())

		var pParsed PayloadAddURL
		err = parsed.GetPayload(&pParsed)
		assert.NoError(t, err)
		assert.Equal(t, "http://io.io/", pParsed.URL)
	})

	t.Run("Parse Invalid Data", func(t *testing.T) {
		parsed, err := Parse([]byte("invalid json"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot deserialize")
		assert.Nil(t, parsed)
	})
}

// 5. Стресс-тест на конкурентность (Data Race Check)
func TestConcurrency(t *testing.T) {
	ev, p, _ := NewEvent[PayloadAddURL]()
	ev.SetPayload(p)

	var wg sync.WaitGroup
	workers := 10000

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				_ = ev.GetID()
				_ = ev.GetType()
				var res PayloadAddURL
				_ = ev.GetPayload(&res)
			} else {
				_ = ev.SetPayload(&PayloadAddURL{ID: uint64(n)})
				_, _ = ev.Serialize()
			}
		}(i)
	}
	wg.Wait()
}
