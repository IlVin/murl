package service

import (
	"errors"
	"murl/internal/config"
	"murl/internal/model"
	"net/url"

	"go.uber.org/zap"
)

var (
	ErrInvalidURL        = errors.New("invalid URL format")
	ErrStoreGetFailed    = errors.New("failed to retrieve data")
	ErrStoreSaveFailed   = errors.New("failed to persist data")
	ErrURLNoAbs          = errors.New("URL is not absolute")
	ErrUnsupportedScheme = errors.New("unsupported protocol scheme")
)

// Объявляем список используемых параметров конфига
type IServiceConfig interface {
	config.IZapLogger
	ShortBaseURL() config.ShortBaseURL
}

type MicroURLRepo interface {
	Save(lURL string) (byte, uint64, error)
	Load(sID byte, idx uint64) (string, error)
}

type Service struct {
	zap          *zap.Logger
	shortBaseURL config.ShortBaseURL
	repo         MicroURLRepo
}

func NewService(cfg IServiceConfig, repo MicroURLRepo) *Service {
	return &Service{
		zap:          cfg.Zap(),
		shortBaseURL: cfg.ShortBaseURL(),
		repo:         repo,
	}
}

func (s *Service) AddURL(longURL string) (string, error) {
	u, err := url.Parse(longURL)
	if err != nil {
		s.zap.Warn("invalid URL provided",
			zap.String("long_url", longURL),
			zap.Error(err),
		)
		return "", errors.Join(ErrInvalidURL, err)
	}
	if !u.IsAbs() {
		s.zap.Warn("longURL is not absolute",
			zap.String("long_url", longURL),
		)
		return "", ErrURLNoAbs
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		s.zap.Warn("invalid scheme",
			zap.String("long_url", longURL),
			zap.String("scheme", u.Scheme),
		)
		return "", ErrUnsupportedScheme
	}

	sID, idx, err := s.repo.Save(u.String())
	if err != nil {
		s.zap.Warn("longURL not saved",
			zap.String("long_url", u.String()),
			zap.Error(err),
		)
		return "", errors.Join(ErrStoreSaveFailed, err)
	}

	sURL, err := model.MakeShortURL(sID, idx, &s.shortBaseURL.URL)
	if err != nil {
		s.zap.Warn("cannot make shortURL",
			zap.Uint8("sID", sID),
			zap.Uint64("idx", idx),
			zap.String("long_url", longURL),
			zap.Error(err),
		)
		return "", errors.Join(ErrStoreSaveFailed, err)
	}

	s.zap.Info("URL shortened",
		zap.Uint8("sID", sID),
		zap.Uint64("idx", idx),
		zap.String("short_url", sURL),
	)

	return sURL, nil
}

func (s *Service) GetURL(sURL string) (string, error) {
	sID, idx, err := model.ParseShortURL(sURL)
	if err != nil {
		s.zap.Debug("bad format incoming shortURL",
			zap.String("sURL", sURL),
			zap.Error(err),
		)
		return "", errors.Join(ErrInvalidURL, err)
	}

	u, err := s.repo.Load(sID, idx)
	if err != nil {
		s.zap.Error("failed to load URL from repo",
			zap.Uint8("sID", sID),
			zap.Uint64("idx", idx),
			zap.Error(err),
		)
		return "", errors.Join(ErrStoreGetFailed, err)
	}

	return u, nil
}
