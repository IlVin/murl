package service

import (
	"errors"
	"log/slog"
	"murl/internal/config"
	"murl/internal/model"
	"net/url"
)

var (
	ErrInvalidURL        = errors.New("invalid URL format")
	ErrStoreGetFailed    = errors.New("failed to retrieve data")
	ErrStoreSaveFailed   = errors.New("failed to persist data")
	ErrURLNoAbs          = errors.New("URL is not absolute")
	ErrUnsupportedScheme = errors.New("unsupported protocol scheme")
)

// Объявляем список используемых параметров конфига
type ServiceConfig interface {
	ShortBaseURL() config.ShortBaseURL
}

type MicroURLRepo interface {
	Save(lURL string) (byte, uint64, error)
	Load(sID byte, idx uint64) (string, error)
}

type Service struct {
	shortBaseURL config.ShortBaseURL
	repo         MicroURLRepo
}

func NewService(cfg ServiceConfig, repo MicroURLRepo) *Service {
	return &Service{
		shortBaseURL: cfg.ShortBaseURL(),
		repo:         repo,
	}
}

func (s *Service) AddURL(longURL string) (string, error) {
	u, err := url.Parse(longURL)
	if err != nil {
		slog.Warn("invalid URL provided",
			slog.String("long_url", longURL),
			slog.Any("err", err),
		)
		return "", errors.Join(ErrInvalidURL, err)
	}
	if !u.IsAbs() {
		slog.Warn("longURL is not absolute",
			slog.String("long_url", longURL),
		)
		return "", ErrURLNoAbs
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		slog.Warn("invalid scheme",
			slog.String("long_url", longURL),
			slog.String("scheme", u.Scheme),
		)
		return "", ErrUnsupportedScheme
	}

	sID, idx, err := s.repo.Save(u.String())
	if err != nil {
		slog.Warn("longURL not saved",
			slog.String("long_url", u.String()),
			slog.Any("err", err),
		)
		return "", errors.Join(ErrStoreSaveFailed, err)
	}

	sURL, err := model.MakeShortURL(sID, idx, &s.shortBaseURL.URL)
	if err != nil {
		slog.Warn("cannot make shortURL",
			slog.Uint64("sID", uint64(sID)),
			slog.Uint64("idx", idx),
			slog.String("long_url", longURL),
			slog.Any("err", err),
		)
		return "", errors.Join(ErrStoreSaveFailed, err)
	}

	slog.Info("URL shortened",
		slog.Uint64("sID", uint64(sID)),
		slog.Uint64("idx", idx),
		slog.String("short_url", sURL),
	)

	return sURL, nil
}

func (s *Service) GetURL(sURL string) (string, error) {
	sID, idx, err := model.ParseShortURL(sURL)
	if err != nil {
		slog.Debug("bad format incoming shortURL",
			slog.String("sURL", sURL),
			slog.Any("err", err),
		)
		return "", errors.Join(ErrInvalidURL, err)
	}

	u, err := s.repo.Load(sID, idx)
	if err != nil {
		slog.Error("failed to load URL from repo",
			slog.Uint64("sID", uint64(sID)),
			slog.Uint64("idx", idx),
			slog.Any("err", err),
		)
		return "", errors.Join(ErrStoreGetFailed, err)
	}

	return u, nil
}
