package repository

import (
	"errors"
)

var (
	ErrURLNotFound         = errors.New("URL not found")
	ErrShortURLAlreadyUsed = errors.New("the short URL is already in use for other long URLs")
	ErrURLMappingNotSaved  = errors.New("the mapping between short URL and long URL is not saved")
)
