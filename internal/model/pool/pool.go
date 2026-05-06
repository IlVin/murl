package pool

// Resettable описывает контракт для объектов, которые можно сбросить.
type Resettable interface {
	Reset()
}

// Pool — структура для хранения объектов одного типа T.
// Тип T ограничен интерфейсом Resettable.
type Pool[T Resettable] struct {
	items []T
}

// New — функция-конструктор, возвращающая указатель на Pool.
func New[T Resettable]() *Pool[T] {
	return &Pool[T]{
		items: make([]T, 0),
	}
}

// Get извлекает объект из пула.
// Если пул пуст, возвращается "нулевое" значение типа T и false.
func (p *Pool[T]) Get() (T, bool) {
	if len(p.items) == 0 {
		var zero T
		return zero, false
	}

	// Достаем последний элемент
	item := p.items[len(p.items)-1]
	p.items = p.items[:len(p.items)-1]
	return item, true
}

// Put сбрасывает состояние объекта и помещает его в пул.
func (p *Pool[T]) Put(item T) {
	item.Reset()
	p.items = append(p.items, item)
}
