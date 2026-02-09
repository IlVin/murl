package service

import (
	"context"
	"fmt"
	"log/slog"
	"murl/internal/config"
	"murl/internal/model"
	"net/url"
)

//go:generate mockgen -source=$GOFILE -destination=service_mocks_test.go -package=$GOPACKAGE

// Объявляем список используемых параметров конфига
type ServiceConfig interface {
	ShortBaseURL() config.ShortBaseURL
}

type MicroURLRepo interface {
	Save(ctx context.Context, lURL string) (byte, uint64, error)
	Load(ctx context.Context, sID byte, idx uint64) (string, error)
	Ping(ctx context.Context) error
}

type Service struct {
	shortBaseURL config.ShortBaseURL
	repo         MicroURLRepo
}

func NewService(ctx context.Context, cfg ServiceConfig, repo MicroURLRepo) *Service {
	return &Service{
		shortBaseURL: cfg.ShortBaseURL(),
		repo:         repo,
	}
}

func (s *Service) AddURL(ctx context.Context, longURL string) (string, error) {
	u, err := url.Parse(longURL)
	if err != nil {
		slog.Debug("invalid URL provided",
			slog.String("long_url", longURL),
			slog.Any("err", err),
		)
		return "", fmt.Errorf("invalid URL format: %w", err)
	}
	if !u.IsAbs() {
		slog.Info("longURL is not absolute",
			slog.String("long_url", longURL),
		)
		return "", fmt.Errorf("URL must be absolute (include scheme)")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		slog.Info("invalid scheme",
			slog.String("long_url", longURL),
			slog.String("scheme", u.Scheme),
		)
		return "", fmt.Errorf("unsupported protocol scheme: %s", u.Scheme)
	}

	normalizedURL := u.String()
	sID, idx, err := s.repo.Save(ctx, normalizedURL)
	if err != nil {
		slog.Error("repo save failed",
			slog.String("url", normalizedURL),
			slog.Any("err", err),
		)
		return "", fmt.Errorf("failed to persist data: %w", err)
	}

	sURL, err := model.MakeShortURL(sID, idx, &s.shortBaseURL.URL)
	if err != nil {
		slog.Warn("cannot make shortURL",
			slog.Uint64("sID", uint64(sID)),
			slog.Uint64("idx", idx),
		)
		return "", fmt.Errorf("failed to generate short URL: %w", err)
	}

	slog.Info("URL shortened",
		slog.Uint64("sID", uint64(sID)),
		slog.Uint64("idx", idx),
		slog.String("short_url", sURL),
	)

	return sURL, nil
}

func (s *Service) GetURL(ctx context.Context, sURL string) (string, error) {
	sID, idx, err := model.ParseShortURL(sURL)
	if err != nil {
		slog.Debug("bad format incoming shortURL",
			slog.String("sURL", sURL),
			slog.Any("err", err),
		)
		return "", fmt.Errorf("invalid URL format: %w", err)
	}

	u, err := s.repo.Load(ctx, sID, idx)
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

func (s *Service) Ping(ctx context.Context) error {
	return s.repo.Ping(ctx)
}
