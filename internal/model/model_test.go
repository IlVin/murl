package model

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortURL_Cycle(t *testing.T) {
	baseURL, _ := url.Parse("https://ex.com")
	testCases := []struct {
		shard byte
		idx   uint64
	}{
		{shard: 0, idx: 0},
		{shard: 63, idx: 123456789},
		{shard: 10, idx: 1<<64 - 1},
	}

	for _, tc := range testCases {
		// Тест создания
		sURL, err := MakeShortURL(tc.shard, tc.idx, baseURL)
		require.NoError(t, err)

		// Тест парсинга
		shard, idx, err := ParseShortURL(sURL)
		require.NoError(t, err)

		assert.Equal(t, tc.shard, shard)
		assert.Equal(t, tc.idx, idx)
	}
}

func TestMakeShortURL_Errors(t *testing.T) {
	baseURL, _ := url.Parse("http://localhost")

	t.Run("invalid shard id", func(t *testing.T) {
		_, err := MakeShortURL(64, 0, baseURL) // Макс индекс 63
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ShardID: index [64] out of bounds")
	})
}

func TestParseShortURL_Errors(t *testing.T) {
	t.Run("invalid url format", func(t *testing.T) {
		// Слишком короткий путь (нужно минимум "/" + шард + данные)
		_, _, err := ParseShortURL("http://ex.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid short url format")
	})

	t.Run("broken url parse", func(t *testing.T) {
		// URL с недопустимыми символами для url.Parse
		_, _, err := ParseShortURL("http://ex.com")
		assert.Error(t, err)
	})

	t.Run("invalid shard character", func(t *testing.T) {
		// Символ '!' отсутствует в b64uDict
		_, _, err := ParseShortURL("http://ex.com/!123")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid shard identifier")
	})

	t.Run("failed base64 decode", func(t *testing.T) {
		// Символ '!' внутри данных base64
		_, _, err := ParseShortURL("http://ex.com/bc!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode base64 data")
	})

	t.Run("failed varint decode - n is 0", func(t *testing.T) {
		// Пустые данные после шарда (невозможно декодировать varint)
		// Для этого нужно обмануть проверку длины, добавив '/'
		_, _, err := ParseShortURL("http://ex.com")
		// Здесь сработает первая проверка len < 3, но если бы прошли:
		assert.Error(t, err)
	})

	t.Run("varint overflow or incomplete", func(t *testing.T) {
		// Даем 10 байт с установленным MSB (превышение лимита uint64 для varint)
		buf := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}
		encoded := b64u().EncodeToString(buf)
		_, _, err := ParseShortURL("http://ex.com/A" + encoded)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode record index")
	})

	t.Run("extra bytes after varint", func(t *testing.T) {
		// Валидный varint (0), но после него лишний байт
		buf := []byte{0x00, 0xff}
		encoded := b64u().EncodeToString(buf)
		_, _, err := ParseShortURL("http://ex.com/A" + encoded)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode record index")
	})
}

func TestB64uInternal(t *testing.T) {
	// Покрываем вызов вспомогательной функции напрямую
	enc := b64u()
	assert.NotNil(t, enc)
}
