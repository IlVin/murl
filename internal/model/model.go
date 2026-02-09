package model

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
)

const b64uDict = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func b64u() *base64.Encoding {
	return base64.URLEncoding.WithPadding(base64.NoPadding)
}

func ParseShortURL(sURL string) (shardID byte, idx uint64, err error) {
	u, err := url.Parse(sURL)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid short url format (%s): %w", sURL, err)
	}
	//  "/" + "." + "S" + "II" (минимум 5 байт)
	if len(u.Path) < 5 || u.Path[0:2] != "/." {
		return 0, 0, fmt.Errorf("invalid short url format: %s", sURL)
	}

	// Извлекаем шард из первого символа после слэша
	shardChar := u.Path[2] // Берем байт, так как словарь ASCII
	pos := strings.IndexByte(b64uDict, shardChar)
	if pos < 0 {
		return 0, 0, fmt.Errorf("invalid shard identifier: '%c'", shardChar)
	}
	shardID = byte(pos)

	// Декодируем индекс из остатка пути
	data, err := b64u().DecodeString(u.Path[3:])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode base64 data: %w", err)
	}

	idx, n := binary.Uvarint(data)
	if n <= 0 || n != len(data) {
		return 0, 0, fmt.Errorf("failed to decode record index: %v", idx)
	}

	return shardID, idx, nil
}

func MakeShortURL(shardID byte, idx uint64, baseURL *url.URL) (string, error) {
	if int(shardID) >= len(b64uDict) {
		return "", fmt.Errorf("invalid ShardID: index [%d] out of bounds [%d]", shardID, len(b64uDict)-1)
	}

	// Кодируем индекс в компактный Varint
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, idx)

	// Формируем путь: 1 символ словаря для шарда + base64 от индекса
	encodedIdx := b64u().EncodeToString(buf[:n])
	shortPath := "." + b64uDict[shardID:shardID+1] + encodedIdx

	// Создаем копию URL, чтобы не мутировать оригинал
	resURL := *baseURL
	resURL = *resURL.JoinPath(shortPath)

	return resURL.String(), nil
}
