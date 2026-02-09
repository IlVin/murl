package model

import (
	"fmt"
	"math"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeAndParseShortURL(t *testing.T) {
	baseURL, _ := url.Parse("http://localhost:8080")

	tests := []struct {
		name    string
		shardID byte
		idx     uint64
	}{
		{"min values", 0, 0},
		{"mid values", 32, 12345},
		{"max shard", 63, 999999},
		{"max uint64", 10, math.MaxUint64},
		{"large values", 63, 123456789012345},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Создаем короткую ссылку
			shortURL, err := MakeShortURL(tt.shardID, tt.idx, baseURL)
			require.NoError(t, err)
			assert.Contains(t, shortURL, "http://localhost:8080/.")

			// 2. Парсим обратно
			gotShard, gotIdx, err := ParseShortURL(shortURL)
			require.NoError(t, err)

			// 3. Проверяем идентичность
			assert.Equal(t, tt.shardID, gotShard)
			assert.Equal(t, tt.idx, gotIdx)
		})
	}
}

func TestParseShortURL_Errors(t *testing.T) {
	tests := []struct {
		name    string
		sURL    string
		wantErr string
	}{
		{
			"invalid url format",
			"http://[invalid-url",
			"invalid short url format",
		},
		{
			"too short path",
			"http://localhost/.", // всего 2 символа в пути после слэша
			"invalid short url format",
		},
		{
			"missing dot prefix",
			"http://localhost/AABC", // нет точки в начале
			"invalid short url format",
		},
		{
			"invalid shard char",
			"http://localhost/.!AA", // '!' нет в словаре
			"invalid shard identifier",
		},
		{
			"invalid base64 data",
			"http://localhost/.A.$.", // битый base64
			"failed to decode base64 data",
		},
		{
			"incomplete varint data",
			"http://localhost/.Aww", // Валидный b64, но битый varint (лишние байты или обрыв)
			"failed to decode record index",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shard, idx, err := ParseShortURL(tt.sURL)
			fmt.Println("====>", err)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Zero(t, shard)
			assert.Zero(t, idx)
		})
	}
}

func TestMakeShortURL_Errors(t *testing.T) {
	baseURL, _ := url.Parse("http://localhost")

	t.Run("shardID out of bounds", func(t *testing.T) {
		_, err := MakeShortURL(64, 100, baseURL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "out of bounds")
	})
}

func TestVarintCompactness(t *testing.T) {
	baseURL, _ := url.Parse("http://l")

	// При малых значениях (0-127) Varint занимает 1 байт.
	// Base64 от 1 байта — это 2 символа.
	// Путь: "." + "S" + "II" = 4 символа. Итого "/.SII" = 5 символов.
	shortURL, err := MakeShortURL(0, 10, baseURL)
	require.NoError(t, err)

	u, _ := url.Parse(shortURL)
	assert.Equal(t, 5, len(u.Path), "Path should be exactly 5 chars long for small IDs (including dot)")
}
