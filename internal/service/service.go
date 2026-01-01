package service

import (
	"errors"
	"murl/internal/model"
	"net/url"
)

var (
	ErrURLBadFormat = errors.New("URL in bad format")
	ErrDataNotLoad  = errors.New("data not load")
	ErrDataNotSave  = errors.New("data not save")
	ErrURLNoAbs     = errors.New("URL is not absolute")
	ErrURLBadScheme = errors.New("URL has bad sheme")
)

// Объявляем список используемых параметров конфига
type ServiceConfig interface {
	ShortBaseURL() string
}

type MicroURLStore interface {
	Save(lURL string) (byte, uint64, error)
	Load(sID byte, idx uint64) (string, error)
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
		return "", errors.Join(ErrURLBadFormat, err)
	}
	if !u.IsAbs() {
		return "", ErrURLNoAbs
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", ErrURLBadScheme
	}

	sID, idx, err := s.store.Save(u.String())
	if err != nil {
		return "", errors.Join(ErrDataNotSave, err)
	}

	// Шаблон для URL
	tURL, err := url.Parse(s.cfg.ShortBaseURL())
	if err != nil {
		return "", errors.Join(ErrURLBadFormat, err)
	}

	sURL, err := model.MakeShortURL(sID, idx, tURL)
	if err != nil {
		return "", errors.Join(ErrDataNotSave, err)
	}

	return sURL, nil
}

func (s *Service) GetURL(sURL string) (string, error) {
	sID, idx, err := model.ParseShortURL(sURL)
	if err != nil {
		return "", errors.Join(ErrURLBadFormat, err)
	}

	u, err := s.store.Load(sID, idx)
	if err != nil {
		return "", errors.Join(ErrDataNotLoad, err)
	}

	return u, nil
}
