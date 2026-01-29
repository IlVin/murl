package model

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeShortURL(t *testing.T) {
	baseURL, _ := url.Parse("http://localhost:8080")

	t.Run("success min values", func(t *testing.T) {
		// Shard 0 ('A') + Idx 0 (varint 0x00 -> base64 "A")
		res, err := MakeShortURL(0, 0, baseURL)
		assert.NoError(t, err)
		assert.Equal(t, "http://localhost:8080/AAA", res)
	})

	t.Run("success max values", func(t *testing.T) {
		// Shard 63 ('_') + Idx Max
		res, err := MakeShortURL(63, 18446744073709551615, baseURL)
		assert.NoError(t, err)
		assert.Contains(t, res, "http://localhost:8080/_")
	})

	t.Run("shard overflow error", func(t *testing.T) {
		_, err := MakeShortURL(64, 1, baseURL)
		assert.ErrorIs(t, err, ErrShardIDLimit)
	})
}

func TestParseShortURL(t *testing.T) {
	t.Run("success round-trip", func(t *testing.T) {
		baseURL, _ := url.Parse("http://localhost:8080")
		originalShard := byte(10)
		originalIdx := uint64(123456789)

		sURL, _ := MakeShortURL(originalShard, originalIdx, baseURL)

		shard, idx, err := ParseShortURL(sURL)
		require.NoError(t, err)
		assert.Equal(t, originalShard, shard)
		assert.Equal(t, originalIdx, idx)
	})

	t.Run("invalid url parse", func(t *testing.T) {
		_, _, err := ParseShortURL(":")
		assert.ErrorIs(t, err, ErrInvalidFormat)
	})

	t.Run("path too short", func(t *testing.T) {
		_, _, err := ParseShortURL("http://localhost/A")
		assert.ErrorIs(t, err, ErrInvalidFormat)
	})

	t.Run("invalid shard character", func(t *testing.T) {
		// Символ '!' не входит в b64uDict
		_, _, err := ParseShortURL("http://localhost/!AA")
		assert.ErrorIs(t, err, ErrInvalidShard)
	})

	t.Run("invalid base64 data (bad symbols)", func(t *testing.T) {
		// '/' — невалидный символ для URLEncoding, вызовет ошибку декодирования
		_, _, err := ParseShortURL("http://localhost/Af///")
		assert.ErrorIs(t, err, ErrDecodeBase64)
	})

	t.Run("invalid varint data (corrupted bytes)", func(t *testing.T) {
		// "gA" — это 0x80 в base64.
		// 0x80 — это начало varint (MSB установлен), но данных дальше нет.
		// n вернет 0 или отрицательное число, что вызовет ErrDecodeIndex.
		_, _, err := ParseShortURL("http://localhost/AgA")
		assert.ErrorIs(t, err, ErrDecodeIndex)
	})
}

func TestB64u(t *testing.T) {
	enc := b64u()
	assert.NotNil(t, enc)
	// Проверка URL-safe символа: 63-й индекс (0x3F) -> '_'
	assert.Equal(t, "_", enc.EncodeToString([]byte{63 << 2})[:1])
}
