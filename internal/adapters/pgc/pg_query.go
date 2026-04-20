package pgc

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
)

// Binder describes a function that maps database row fields to a struct T.
// It must return a slice of pointers to the struct fields.
type Binder[T any] func(*T) []any

// Query is a static manifest of an SQL query.
// It implements the PgQuery interface and contains metadata calculated at startup.
type Query[T any] struct {
	name       string
	sql        string
	binder     Binder[T]
	isReadOnly bool
}

// SQL returns the raw SQL query string.
func (q *Query[T]) SQL() string { return q.sql }

// Name returns the auto-generated or manual name of the query for logging and tracing.
func (q *Query[T]) Name() string { return q.name }

// IsReadOnly returns true if the query is marked as a read-only operation.
func (q *Query[T]) IsReadOnly() bool { return q.isReadOnly }

// HasReturns returns true if the query expects to return data (has a binder).
func (q *Query[T]) HasReturns() bool { return q.binder != nil }

// NewTarget creates a new instance of the underlying struct T.
func (q *Query[T]) NewTarget() any { return new(T) }

// Binder performs a type-safe cast of the target and returns the mapping slice for scanning.
func (q *Query[T]) Binder(target any) []any {
	ptr, ok := target.(*T)
	if q.binder == nil || !ok {
		return nil
	}
	return q.binder(ptr)
}

// NewQuery creates a typed query description.
// It uses reflection once at startup to determine the type name and caller location.
func NewQuery[T any](sql string, binder Binder[T]) *Query[T] {
	if sql == "" {
		panic("pgc: sql query cannot be empty")
	}

	typ := reflect.TypeFor[T]()
	for typ.Kind() == reflect.Ptr {
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

// NewCommand is a helper for non-returning queries like INSERT, UPDATE, or DELETE.
// It uses struct{} to minimize memory footprint.
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

// AsWrite marks the query as data-modifying, typically targeting the master node.
func (q *Query[T]) AsWrite() *Query[T] { q.isReadOnly = false; return q }

// AsRead marks the query as read-only, typically targeting replica nodes.
func (q *Query[T]) AsRead() *Query[T] { q.isReadOnly = true; return q }
