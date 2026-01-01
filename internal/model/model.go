package model

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

const b64uDict string = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

var (
	ErrShardIDOverflow error = errors.New("ShardID overflow (must be less than " + strconv.Itoa(len(b64uDict)) + ")")
	ErrParseURL        error = errors.New("failed to parse URL")
	ErrBadB64U         error = errors.New("BASE64u string is corrupted")
	ErrBadBinIdx       error = errors.New("binary idx is corrupted")
	ErrBadShardID      error = errors.New("binary ShardID is corrupted")
)

func b64u() *base64.Encoding {
	return base64.URLEncoding.WithPadding(base64.NoPadding)
}

func ParseShortURL(sURL string) (shardID byte, idx uint64, err error) {
	u, err := url.Parse(sURL)
	if err != nil {
		return 0, 0, errors.Join(ErrParseURL, err)
	}

	pos := strings.Index(b64uDict, u.Path[1:2])
	if pos < 0 {
		return 0, 0, ErrBadShardID
	}
	shardID = byte(pos)

	data, err := b64u().DecodeString(u.Path[2:])
	if err != nil {
		return 0, 0, errors.Join(ErrBadB64U, err)
	}
	idx, n := binary.Uvarint(data)
	if n != len(data) {
		return 0, 0, ErrBadBinIdx
	}

	return shardID, idx, nil
}

func MakeShortURL(shardID byte, idx uint64, tURL *url.URL) (string, error) {
	if int(shardID) >= len(b64uDict) {
		return "", ErrShardIDOverflow
	}
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, idx)
	tURL.Path = "/" + b64uDict[shardID:shardID+1] + b64u().EncodeToString(buf[:n])

	return tURL.String(), nil
}
