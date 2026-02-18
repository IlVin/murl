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

var ErrInvalidURLFormat = errors.New("invalid URL format")
var ErrDomainIsBlocked = errors.New("domain is blocked")
var ErrConflict = errors.New("URL is already shortened")
var ErrQuotaReached = errors.New("quota reached")

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

func (s *Service) NormalizeURL(ctx context.Context, longURL string) (*url.URL, error) {
	u, err := url.Parse(longURL)
	if err != nil {
		slog.Debug("invalid URL provided",
			slog.String("long_url", longURL),
			slog.Any("err", err),
		)
		return nil, errors.Join(ErrInvalidURLFormat, err)
	}
	if !u.IsAbs() {
		slog.Info("longURL is not absolute",
			slog.String("long_url", longURL),
		)
		return nil, errors.Join(ErrInvalidURLFormat, fmt.Errorf("URL must be absolute (include scheme)"))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		slog.Info("invalid scheme",
			slog.String("long_url", longURL),
			slog.String("scheme", u.Scheme),
		)
		return nil, errors.Join(ErrInvalidURLFormat, fmt.Errorf("unsupported protocol scheme: %s", u.Scheme))
	}

	return u, nil
}

func (s *Service) AddURL(ctx context.Context, longURL string) (string, error) {

	// Нормализация URL
	normalizedURL, err := s.NormalizeURL(ctx, longURL)
	if err != nil {
		return "", fmt.Errorf("cannot normalize long URL: %w", err)
	}

	// Предохранитель от циклических сокращений
	if s.shortBaseURL.URL.Hostname() == normalizedURL.Hostname() {
		return "", ErrDomainIsBlocked
	}

	// Запись URL в БД
	sID, idx, conflictFlag, err := s.repo.Save(ctx, normalizedURL.String())
	if err != nil {
		return "", fmt.Errorf("failed to persist data: %w", err)
	}

	// Генерируем короткий URL
	sURL, err := model.MakeShortURL(sID, idx, &s.shortBaseURL.URL)
	if err != nil {
		return "", fmt.Errorf("failed to generate short URL: %w", err)
	}

	if conflictFlag {
		return sURL, fmt.Errorf("long URL %s already shortened to short URL %s: %w", normalizedURL.String(), sURL, ErrConflict)
	}

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
			p[i].Err = ErrInvalidURLFormat.Error()
			pRes = append(pRes, p[i])
		} else {
			p[i].OrigURL = normalizedURL.String()
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
			rBatch[i].Err = ErrInvalidURLFormat.Error()
		} else {
			rBatch[i].OrigURL = ""
			rBatch[i].ShortURL = sURL
		}
	}

	pRes = append(pRes, rBatch...)

	return event.MakeEvent(pRes, e)
}
