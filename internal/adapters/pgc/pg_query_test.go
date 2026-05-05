package pgc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type testModel struct {
	ID   int64
	Data string
}

func testBinder(t *testModel) []any {
	return []any{&t.ID, &t.Data}
}

func TestNewQuery(t *testing.T) {

	t.Run("auto name generation", func(t *testing.T) {
		q := NewQuery[testModel]("SELECT 1", testBinder)

		// Имя должно содержать название структуры и текущий файл
		assert.Contains(t, q.Name(), "testModel")
		assert.Contains(t, q.Name(), "pg_query_test.go")
	})

	t.Run("default is write", func(t *testing.T) {
		q := NewQuery[testModel]("SELECT 1", testBinder)
		assert.False(t, q.IsReadOnly())
	})

	t.Run("anonymous type name", func(t *testing.T) {
		q := NewQuery[int]("SELECT 1", nil)
		// Для встроенных типов или анонимных структур может быть "int" или "Anon"
		assert.Contains(t, q.Name(), "int")
	})
}

func TestQuery_StateModifiers(t *testing.T) {
	t.Run("AsRead changes state", func(t *testing.T) {
		q := NewQuery[testModel]("SELECT 1", testBinder).AsRead()
		assert.True(t, q.IsReadOnly())
	})

	t.Run("AsWrite changes state", func(t *testing.T) {
		q := NewQuery[testModel]("SELECT 1", testBinder).AsRead().AsWrite()
		assert.False(t, q.IsReadOnly())
	})
}

func TestQuery_InterfaceImplementation(t *testing.T) {
	sql := "SELECT id, data FROM table"
	q := NewQuery[testModel](sql, testBinder)

	t.Run("SQL return", func(t *testing.T) {
		assert.Equal(t, sql, q.SQL())
	})

	t.Run("HasReturns logic", func(t *testing.T) {
		assert.True(t, q.HasReturns())

		qNoReturn := NewQuery[any]("DELETE FROM table", nil)
		assert.False(t, qNoReturn.HasReturns())
	})

	t.Run("NewTarget and Binder", func(t *testing.T) {
		// Создаем новый объект через фабрику
		target := q.NewTarget()
		assert.IsType(t, &testModel{}, target)

		// Проверяем, что Binder корректно маппит поля
		fields := q.Binder(target)
		assert.Len(t, fields, 2)

		// Проверяем работоспособность указателей через Binder
		idVal := int64(100)
		*(fields[0].(*int64)) = idVal

		assert.Equal(t, idVal, target.(*testModel).ID)
	})

	t.Run("Binder with nil", func(t *testing.T) {
		qNil := NewQuery[any]("INSERT...", nil)
		assert.Nil(t, qNil.Binder(new(any)))
	})
}

func TestQuery_PointerType(t *testing.T) {
	// Проверяем, что если T это указатель, имя все равно извлекается корректно
	q := NewQuery[*testModel]("SELECT 1", nil)
	assert.Contains(t, q.Name(), "testModel")
}

// User — тестовая структура для бенчмарков
type UserBench struct {
	ID    int64
	Email string
	Age   int
}

// BenchmarkNewQuery измеряет накладные расходы на создание манифеста запроса.
// Включает в себя рефлексию для получения имени типа и runtime.Caller для локации.
func BenchmarkNewQuery(b *testing.B) {
	sql := "SELECT id, email, age FROM users WHERE id = $1"
	binder := func(u *UserBench) []any {
		return []any{&u.ID, &u.Email, &u.Age}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewQuery(sql, binder)
	}
}

// BenchmarkQuery_Binder измеряет скорость типизированного связывания полей.
// Это имитация того, что происходит внутри каждой итерации при сканировании строк из БД.
func BenchmarkQuery_Binder(b *testing.B) {
	q := NewQuery("SELECT...", func(u *UserBench) []any {
		return []any{&u.ID, &u.Email, &u.Age}
	})

	target := q.NewTarget() // Создаем один раз

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Имитируем вызов драйвером для каждой строки
		fields := q.Binder(target)
		if len(fields) != 3 {
			b.Fatal("invalid binder output")
		}
	}
}

// BenchmarkNewCommand измеряет скорость создания простых команд без биндера.
func BenchmarkNewCommand(b *testing.B) {
	sql := "DELETE FROM users WHERE id = $1"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewCommand(sql)
	}
}
