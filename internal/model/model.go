// Package model содержит базовые доменные модели и вспомогательные функции
// для работы с короткими путями и алгоритмами шардирования.
package model

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"murl/internal/config"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/google/uuid"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=model_mock_test.go -package=$GOPACKAGE

// Session представляет активную сессию пользователя в системе.
type Session struct {
	ID  uuid.UUID `json:"id"`
	TTL time.Time `json:"ttl"`
}

// shortPathMarker — префикс, позволяющий однозначно идентифицировать короткую ссылку в URL.
const shortPathMarker = "/."

// b64uDict — алфавит, используемый для кодирования шардов и индексов (URL-safe base64).
const b64uDict = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// b64u возвращает настроенный кодировщик Base64 без дополнения (padding).
func b64u() *base64.Encoding {
	return base64.URLEncoding.WithPadding(base64.NoPadding)
}

// ParseShortPath извлекает идентификатор шарда и порядковый индекс записи из короткого пути.
// Путь должен иметь формат: "/." + {shard_char} + {base64_uvarint}.
func ParseShortPath(shortPath string) (byte, uint64, error) {
	n := strings.LastIndex(shortPath, shortPathMarker)
	if n < 0 {
		return 0, 0, fmt.Errorf("invalid shortPath format: cannot find the substring '%s' in '%s'", shortPathMarker, shortPath)
	}
	shortPath = shortPath[n:]

	//  "/" + "." + "S" + "II" (минимум 5 байт)
	if len(shortPath) < 5 {
		return 0, 0, fmt.Errorf("invalid short path format: %s", shortPath)
	}

	// Извлекаем шард из первого символа после слэша
	shardChar := shortPath[2] // Берем байт, так как словарь ASCII
	pos := strings.IndexByte(b64uDict, shardChar)
	if pos < 0 {
		return 0, 0, fmt.Errorf("invalid shard identifier: '%c'", shardChar)
	}
	shardID := byte(pos)

	// Декодируем индекс из остатка пути
	data, err := b64u().DecodeString(shortPath[3:])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode base64 data: %w", err)
	}

	idx, n := binary.Uvarint(data)
	if n <= 0 || n != len(data) {
		return 0, 0, fmt.Errorf("failed to decode record index: %v", n)
	}

	return shardID, idx, nil
}

// MakeShortPath формирует компактную строку короткого пути на основе ID шарда и индекса записи.
func MakeShortPath(shardID byte, idx uint64) (string, error) {
	if int(shardID) >= len(b64uDict) {
		return "", fmt.Errorf("invalid ShardID: index [%d] out of bounds [%d]", shardID, len(b64uDict)-1)
	}

	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, idx)

	// Формируем путь: 1 символ словаря для шарда + base64 от индекса
	encodedIdx := b64u().EncodeToString(buf[:n])
	shortPath := shortPathMarker + b64uDict[shardID:shardID+1] + encodedIdx

	return shortPath, nil
}

// Hashable — интерфейс для объектов, которые реализуют собственную логику вычисления хеша.
type Hashable interface {
	Hash() uint64
}

// Integer — ограничение типа (type constraint), включающее все стандартные целочисленные типы Go.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// ShardIDString вычисляет ID шарда для строкового ключа с использованием xxHash.
func ShardIDString(key string, clusterSz byte) byte {
	if clusterSz <= 1 {
		return 0
	}
	return byte(xxhash.Sum64String(key) % uint64(clusterSz))
}

// ShardIDStringer вычисляет ID шарда для объектов, реализующих интерфейс fmt.Stringer.
func ShardIDStringer[T fmt.Stringer](key T, clusterSz byte) byte {
	if clusterSz <= 1 {
		return 0
	}
	return byte(xxhash.Sum64String(key.String()) % uint64(clusterSz))
}

// ShardIDInt вычисляет ID шарда для целочисленных типов с минимальными аллокациями.
func ShardIDInt[T Integer](key T, clusterSz byte) byte {
	if clusterSz <= 1 {
		return 0
	}
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(key))
	return byte(xxhash.Sum64(b[:]) % uint64(clusterSz))
}

// ShardIDHashable вычисляет ID шарда для объектов, реализующих интерфейс Hashable.
func ShardIDHashable[T Hashable](key T, clusterSz byte) byte {
	if clusterSz <= 1 {
		return 0
	}
	return byte(key.Hash() % uint64(clusterSz))
}

// ShardID — универсальная функция для определения целевого шарда для ключа любого типа.
// Использует оптимизированные пути для строк, чисел и известных интерфейсов.
func ShardID[T any](key T, clusterSz byte) byte {
	if clusterSz <= 1 {
		return 0
	}

	switch v := any(key).(type) {
	case string:
		return ShardIDString(v, clusterSz)

	case Hashable:
		return ShardIDHashable(v, clusterSz)

	case fmt.Stringer:
		return ShardIDStringer(v, clusterSz)

	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return ShardIDInt(castToUint64(v), clusterSz)

	default:
		// Медленный путь для всех остальных типов через fmt.Sprint
		return byte(xxhash.Sum64String(fmt.Sprint(v)) % uint64(clusterSz))
	}
}

// castToUint64 Вспомогательная функция для безопасного каста внутри switch
func castToUint64(v any) uint64 {
	switch i := v.(type) {
	case int:
		return uint64(i)
	case int8:
		return uint64(i)
	case int16:
		return uint64(i)
	case int32:
		return uint64(i)
	case int64:
		return uint64(i)
	case uint:
		return uint64(i)
	case uint8:
		return uint64(i)
	case uint16:
		return uint64(i)
	case uint32:
		return uint64(i)
	case uint64:
		return i
	default:
		return 0
	}
}

// GetSession извлекает объект сессии из контекста по ключу, заданному в конфигурации.
func GetSession(ctx context.Context, keySession config.KeySession) (Session, bool) {
	s, ok := ctx.Value(keySession).(Session)
	return s, ok
}
