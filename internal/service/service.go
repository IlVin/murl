package service

import (
	"fmt"
	"log/slog"
	"murl/internal/config"
	"murl/internal/model"
	"net/url"
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
		return "", fmt.Errorf("invalid URL format: %w", err)
	}
	if !u.IsAbs() {
		slog.Warn("longURL is not absolute",
			slog.String("long_url", longURL),
		)
		return "", fmt.Errorf("URL is not absolute")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		slog.Warn("invalid scheme",
			slog.String("long_url", longURL),
			slog.String("scheme", u.Scheme),
		)
		return "", fmt.Errorf("unsupported protocol scheme")
	}

	sID, idx, err := s.repo.Save(u.String())
	if err != nil {
		slog.Warn("longURL not saved",
			slog.String("long_url", u.String()),
			slog.Any("err", err),
		)
		return "", fmt.Errorf("failed to persist data: %w", err)
	}

	sURL, err := model.MakeShortURL(sID, idx, &s.shortBaseURL.URL)
	if err != nil {
		slog.Warn("cannot make shortURL",
			slog.Uint64("sID", uint64(sID)),
			slog.Uint64("idx", idx),
			slog.String("long_url", longURL),
			slog.Any("err", err),
		)
		return "", fmt.Errorf("failed to persist data: %w", err)
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
		return "", fmt.Errorf("invalid URL format: %w", err)
	}

	u, err := s.repo.Load(sID, idx)
	if err != nil {
		slog.Error("failed to load URL from repo",
			slog.Uint64("sID", uint64(sID)),
			slog.Uint64("idx", idx),
			slog.Any("err", err),
		)
		return "", fmt.Errorf("failed to retrieve data: %w", err)
	}

	return u, nil
}
