package pgc

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
)

// Binder описывает функцию, которая сопоставляет поля строки базы данных со структурой T.
// Она должна возвращать срез указателей на поля структуры для последующего сканирования (Scan).
type Binder[T any] func(*T) []any

// Query представляет собой статический манифест SQL-запроса.
// Реализует интерфейс PgQuery и содержит метаданные, вычисляемые при запуске приложения.
type Query[T any] struct {
	name       string
	sql        string
	binder     Binder[T]
	isReadOnly bool
}

// SQL возвращает строку SQL-запроса.
func (q *Query[T]) SQL() string { return q.sql }

// Name возвращает имя запроса, сформированное автоматически или вручную, для логирования и трейсинга.
func (q *Query[T]) Name() string { return q.name }

// IsReadOnly возвращает true, если операция помечена как "только для чтения".
func (q *Query[T]) IsReadOnly() bool { return q.isReadOnly }

// HasReturns возвращает true, если запрос ожидает возврата данных (имеет Binder).
func (q *Query[T]) HasReturns() bool { return q.binder != nil }

// NewTarget создает новый экземпляр (указатель) базовой структуры T.
func (q *Query[T]) NewTarget() any { return new(T) }

// Binder выполняет приведение типа target к *T и возвращает срез полей для сканирования.
func (q *Query[T]) Binder(target any) []any {
	ptr, ok := target.(*T)
	if q.binder == nil || !ok {
		return nil
	}
	return q.binder(ptr)
}

// NewQuery создает типизированное описание запроса.
// Использует рефлексию один раз при инициализации для определения имени типа и места вызова.
func NewQuery[T any](sql string, binder Binder[T]) *Query[T] {
	typ := reflect.TypeFor[T]()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	typeName := typ.Name()
	if typeName == "" {
		typeName = typ.String()
	}

	_, fullPath, line, ok := runtime.Caller(1)
	callerInfo := "unknown"
	if ok {
		dir := filepath.Base(filepath.Dir(fullPath))
		file := filepath.Base(fullPath)
		callerInfo = fmt.Sprintf("%s/%s:%d", dir, file, line)
	}

	return &Query[T]{
		name:   fmt.Sprintf("%s (%s)", typeName, callerInfo),
		sql:    sql,
		binder: binder,
	}
}

// NewCommand — вспомогательная функция для запросов, не возвращающих данные (INSERT, UPDATE, DELETE).
// Использует пустую структуру struct{} для минимизации аллокаций.
func NewCommand(sql string) *Query[struct{}] {
	_, fullPath, line, ok := runtime.Caller(1)
	callerInfo := "unknown"
	if ok {
		dir := filepath.Base(filepath.Dir(fullPath))
		file := filepath.Base(fullPath)
		callerInfo = fmt.Sprintf("%s/%s:%d", dir, file, line)
	}

	return &Query[struct{}]{
		name:   fmt.Sprintf("Command (%s)", callerInfo),
		sql:    sql,
		binder: nil,
	}
}

// AsWrite помечает запрос как изменяющий данные. Обычно такие запросы направляются на master-узел.
func (q *Query[T]) AsWrite() *Query[T] { q.isReadOnly = false; return q }

// AsRead помечает запрос как доступный только для чтения. Может быть направлен на реплику.
func (q *Query[T]) AsRead() *Query[T] { q.isReadOnly = true; return q }
