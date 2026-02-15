package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/config"
	"murl/internal/model"
	"murl/internal/model/event"
	"net/url"
)

//go:generate mockgen -source=$GOFILE -destination=service_mocks_test.go -package=$GOPACKAGE

const errBadURLFormat string = "bad URL format"

// Объявляем список используемых параметров конфига
type ServiceConfig interface {
	ShortBaseURL() config.ShortBaseURL
}

type MicroURLRepo interface {
	Save(ctx context.Context, lURL string) (byte, uint64, bool, error)
	Batch(ctx context.Context, e event.PayloadBatch) (event.PayloadBatch, error)
	Load(ctx context.Context, sID byte, idx uint64) (string, error)
	Ping(ctx context.Context) error
	PushEvent(ctx context.Context, e event.Event)
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

func (s *Service) NormalizeURL(ctx context.Context, longURL string) (string, error) {
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

	return u.String(), nil
}

func (s *Service) AddURL(ctx context.Context, longURL string) (string, bool, error) {

	normalizedURL, err := s.NormalizeURL(ctx, longURL)
	if err != nil {
		return "", false, err
	}

	sID, idx, cf, errSave := s.repo.Save(ctx, normalizedURL)

	if errSave != nil {
		slog.Error("repo save failed",
			slog.String("url", normalizedURL),
			slog.Any("err", errSave),
		)
		return "", false, fmt.Errorf("failed to persist data: %w", err)
	}

	sURL, err := model.MakeShortURL(sID, idx, &s.shortBaseURL.URL)
	if err != nil {
		slog.Warn("cannot make shortURL",
			slog.Uint64("sID", uint64(sID)),
			slog.Uint64("idx", idx),
		)
		return "", false, fmt.Errorf("failed to generate short URL: %w", err)
	}

	slog.Info("URL shortened",
		slog.Uint64("sID", uint64(sID)),
		slog.Uint64("idx", idx),
		slog.String("short_url", sURL),
	)

	return sURL, cf, errSave
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

// Batch получает на вход событие типа event.EvBatch и возращает готовое
func (s *Service) Batch(ctx context.Context, e event.Event) (event.Event, error) {

	if e.GetType() != event.EvBatch {
		return nil, errors.New("incompatible event type: want event.EvBatch")
	}

	// p исходный пэйлоад
	p := event.PayloadBatch{}
	if err := e.GetPayload(&p); err != nil {
		return nil, fmt.Errorf("cannot get payload: %w", err)
	}

	// pRes результирующий пэйлоад
	pRes := make(event.PayloadBatch, 0, len(p))

	// rBatch пэйлоад, который нужно отправить в repo
	rBatch := make(event.PayloadBatch, 0, len(p))

	// Нормализация OrigURL
	for i := range p {
		normalizedURL, err := s.NormalizeURL(ctx, p[i].OrigURL)
		if err != nil {
			p[i].Err = errBadURLFormat
			pRes = append(pRes, p[i])
		} else {
			p[i].OrigURL = normalizedURL
			rBatch = append(rBatch, p[i])
		}
	}

	// Записываем в хранилище
	rBatch, err := s.repo.Batch(ctx, rBatch)
	if err != nil {
		return nil, err
	}

	// Записываем в WAL
	eWAL, err := event.MakeEvent(rBatch, e)
	if err != nil {
		return nil, fmt.Errorf("batch URL WAL event was not create: %w", err)
	}
	s.repo.PushEvent(ctx, eWAL)

	// Генерируем ShortURL
	for i := range rBatch {
		sURL, err := model.MakeShortURL(rBatch[i].ShardID, rBatch[i].Idx, &s.shortBaseURL.URL)
		if err != nil {
			slog.Warn("cannot make shortURL",
				slog.Uint64("sID", uint64(rBatch[i].ShardID)),
				slog.Uint64("idx", rBatch[i].Idx),
			)
			rBatch[i].Err = errBadURLFormat
		} else {
			rBatch[i].OrigURL = ""
			rBatch[i].ShortURL = sURL
		}
	}

	pRes = append(pRes, rBatch...)

	return event.MakeEvent(pRes, e)
}
