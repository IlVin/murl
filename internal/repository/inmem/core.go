// Package inmem предоставляет реализацию репозитория в оперативной памяти (In-Memory).
// Использует шардирование для обеспечения высокой производительности в многопоточной среде.
package inmem

import (
	"errors"
	"fmt"
	"murl/internal/model"
	"sync"

	"golang.org/x/sys/cpu"
)

// =============================================
//
//	InMemCore
//
// =============================================

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=core_mock_test.go -package=$GOPACKAGE

// InMemConfig определяет интерфейс настроек, необходимых для инициализации
// In-Memory хранилища.
type InMemConfig interface {
	ShardSize() byte
}

// InMemShard представляет собой атомарную единицу хранения данных (шард).
// Включает в себя механизмы защиты от False Sharing (выравнивание по Cache Line).
type InMemShard struct {
	_ sync.Locker // фиктивный интерфейс для go vet
	// Mu защищает данные конкретного шарда.
	Mu sync.RWMutex
	_  cpu.CacheLinePad
	// Data хранит маппинг: Порядковый индекс -> Оригинальный URL.
	Data map[uint64]string
	// Index хранит обратный индекс: Оригинальный URL -> Порядковый индекс (для быстрого поиска дубликатов).
	Index map[string]uint64
	// Sessions хранит привязку идентификаторов ссылок к сессиям пользователей.
	Sessions map[string][]uint64
	// LastIdx — последний использованный порядковый индекс в данном шарде.
	LastIdx uint64
	_       cpu.CacheLinePad
}

// InMemCore — это ядро системы хранения, управляющее массивом шардов.
type InMemCore struct {
	shards []InMemShard
}

// NewInMemCore создает и инициализирует новый экземпляр In-Memory хранилища.
// Выделяет память под карты (maps) во всех шардах согласно конфигурации.
func NewInMemCore(cfg InMemConfig) (*InMemCore, error) {
	shardSize := int(cfg.ShardSize())
	if shardSize <= 0 {
		return nil, errors.New("shard size must be greater than 0")
	}

	s := &InMemCore{
		shards: make([]InMemShard, shardSize),
	}

	for i := range shardSize {
		s.shards[i].Data = make(map[uint64]string, defaultCap)
		s.shards[i].Index = make(map[string]uint64, defaultCap)
		s.shards[i].Sessions = make(map[string][]uint64, defaultCap)
	}

	return s, nil
}

// Size возвращает текущее количество шардов в хранилище.
func (c *InMemCore) Size() byte {
	return byte(len(c.shards))
}

// ShardID вычисляет идентификатор целевого шарда для любого типа ключа,
// используя алгоритм распределения из пакета model.
func (c *InMemCore) ShardID(key any) byte {
	return model.ShardID(key, c.Size())
}

// GetShard возвращает указатель на структуру конкретного шарда по его идентификатору.
// Возвращает ошибку, если shardID выходит за пределы допустимого диапазона.
func (c *InMemCore) GetShard(shardID byte) (*InMemShard, error) {
	if shardID >= c.Size() {
		return nil, fmt.Errorf("shardID [%d] out of range [0, .., %d)", shardID, c.Size())
	}
	return &c.shards[shardID], nil
}
