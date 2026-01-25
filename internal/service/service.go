package service

import (
	"errors"
	"net/url"
)

var (
	ErrURLBadFormat = errors.New("URL in bad format")
	ErrURLNoAbs     = errors.New("URL is not absolute")
	ErrURLBadScheme = errors.New("URL has bad sheme")
)

// Объявляем список используемых параметров конфига
type ServiceConfig interface {
	ShortURLHostAndPort() string
}

type MicroURLStore interface {
	Save(string) (string, error)
	Load(string) (string, error)
}

type Service struct {
	cfg   ServiceConfig
	store MicroURLStore
}

func NewService(cfg ServiceConfig, store MicroURLStore) *Service {
	return &Service{
		cfg:   cfg,
		store: store,
	}
}

func (s *Service) AddURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", ErrURLBadFormat
	}
	if !u.IsAbs() {
		return "", ErrURLNoAbs
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", ErrURLBadScheme
	}

	shortPath, err := s.store.Save(u.String())
	if err != nil {
		return "", err
	}

	shortURL := &url.URL{
		Scheme: "http",
		Host:   s.cfg.ShortURLHostAndPort(),
		Path:   shortPath,
	}

	return shortURL.String(), nil
}

func (s *Service) GetURL(shortID string) (string, error) {
	u, err := s.store.Load(shortID)
	if err != nil {
		return "", err
	}

	return u, nil
}
