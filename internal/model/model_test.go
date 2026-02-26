package model

import (
	"errors"
	"testing"
)

// Mock для Hashable
type mockHashable struct{ val uint64 }

func (m mockHashable) Hash() uint64 { return m.val }

// Mock для Stringer
type mockStringer struct{ val string }

func (m mockStringer) String() string { return m.val }

func TestParseShortPath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantShard byte
		wantIdx   uint64
		wantErr   bool
	}{
		// 'B' (шард 1) + 'MA' (число 48 в base64u)
		{"Valid Short", "/.BMA", 1, 48, false},

		// 'A' (шард 0) + 'AA' (число 0 в base64u)
		{"Shortest possible", "/.AAA", 0, 0, false},

		{"With prefix", "https://site.com", 1, 12, true},
		{"No marker", "invalid/path", 0, 0, true},

		// Теперь это ошибка, так как индекс 'A' (1 символ) невозможен для base64
		{"Too short index", "/.BA", 0, 0, true},

		{"Invalid shard char", "/.!AA", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, i, err := ParseShortPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("%s: error = %v, wantErr %v", tt.name, err, tt.wantErr)
				return
			}
			if !tt.wantErr && (s != tt.wantShard || i != tt.wantIdx) {
				t.Errorf("%s: got shard %d idx %d, want %d idx %d", tt.name, s, i, tt.wantShard, tt.wantIdx)
			}
		})
	}
}

func TestMakeShortPath(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		path, err := MakeShortPath(1, 10)
		if err != nil || path != "/.BCg" {
			t.Errorf("Unexpected result: %s, %v", path, err)
		}
	})

	t.Run("Invalid ShardID", func(t *testing.T) {
		_, err := MakeShortPath(64, 0)
		if err == nil {
			t.Error("Expected error for shardID 64, got nil")
		}
	})
}

func TestShardID_AllTypes(t *testing.T) {
	const sz byte = 10

	t.Run("ClusterSize 1", func(t *testing.T) {
		if ShardID("any", 1) != 0 {
			t.Error("Should return 0 for clusterSz <= 1")
		}
	})

	t.Run("Types", func(t *testing.T) {
		// String
		_ = ShardID("test", sz)
		// Hashable
		_ = ShardID(mockHashable{100}, sz)
		// Stringer
		_ = ShardID(mockStringer{"test"}, sz)
		// Integers (trigger castToUint64)
		_ = ShardID(int(1), sz)
		_ = ShardID(int8(1), sz)
		_ = ShardID(int16(1), sz)
		_ = ShardID(int32(1), sz)
		_ = ShardID(int64(1), sz)
		_ = ShardID(uint(1), sz)
		_ = ShardID(uint8(1), sz)
		_ = ShardID(uint16(1), sz)
		_ = ShardID(uint32(1), sz)
		_ = ShardID(uint64(1), sz)
		_ = ShardID(uintptr(1), sz)
		// Default (fallback to fmt.Sprint)
		_ = ShardID(errors.New("err"), sz)
		_ = ShardID(struct{ A int }{1}, sz)
	})
}

func TestCastToUint64_Default(t *testing.T) {
	// Проверка ветки default в вспомогательной функции
	res := castToUint64("not a number")
	if res != 0 {
		t.Errorf("Expected 0 for non-numeric type, got %d", res)
	}
}
