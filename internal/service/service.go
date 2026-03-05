package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/config"
	"murl/internal/model"
	"murl/internal/model/event"
	"murl/internal/repository"
	"net/url"
)

var ErrInvalidURLFormat = errors.New("invalid URL format")
var ErrDomainIsBlocked = errors.New("domain is blocked")
var ErrConflict = errors.New("URL is already shortened")
var ErrQuotaReached = errors.New("quota reached")
var ErrNoContent = errors.New("no content")
var ErrGone = errors.New("gone")

// Объявляем список используемых параметров конфига
type ServiceConfig interface {
	ShortBaseURL() config.ShortBaseURL
	KeySession() config.KeySession
}

type Service struct {
	shortBaseURL config.ShortBaseURL
	repo         repository.Repo
	keySession   config.KeySession
}

func NewService(ctx context.Context, cfg ServiceConfig, repo repository.Repo) *Service {
	return &Service{
		shortBaseURL: cfg.ShortBaseURL(),
		repo:         repo,
		keySession:   cfg.KeySession(),
	}
}

func (s *Service) NormalizeURL(ctx context.Context, originalURL string) (*url.URL, error) {
	u, err := url.Parse(originalURL)
	if err != nil {
		slog.Debug("invalid URL provided",
			slog.String("original_url", originalURL),
			slog.Any("err", err),
		)
		return nil, errors.Join(ErrInvalidURLFormat, err)
	}
	if !u.IsAbs() {
		slog.Info("originalURL is not absolute",
			slog.String("original_url", originalURL),
		)
		return nil, errors.Join(ErrInvalidURLFormat, fmt.Errorf("URL must be absolute (include scheme)"))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		slog.Info("invalid scheme",
			slog.String("original_url", originalURL),
			slog.String("scheme", u.Scheme),
		)
		return nil, errors.Join(ErrInvalidURLFormat, fmt.Errorf("unsupported protocol scheme: %s", u.Scheme))
	}

	return u, nil
}

func (s *Service) DeleteURLBySessionID(ctx context.Context, session model.Session, data []string) error {
	shortURLs := make([]string, 0, len(data))
	for i := range data {
		shortURLs = append(shortURLs, "/"+data[i])
	}
	e, err := event.MakeEvent(event.PayloadDeleteURLBySessionID{
		SessionID: session.ID,
		ShortURLs: shortURLs,
	}, nil)
	if err != nil {
		return fmt.Errorf("make EvDeleteURLBySessionID fail: %w", err)
	}

	slog.Info("service.DeleteURLBySessionID",
		slog.Any("sessionID", session.ID),
		slog.Any("shortURLs", shortURLs),
	)

	if _, err := s.repo.On(ctx, e); err != nil {
		return fmt.Errorf("on EvDeleteURLBySessionID fail: %w", err)
	}

	return nil
}

func (s *Service) GetURLBySessionID(ctx context.Context, session model.Session) (*event.PayloadGetURLBySessionID, error) {
	e, err := event.MakeEvent(event.PayloadGetURLBySessionID{SessionID: session.ID}, nil)
	if err != nil {
		return nil, fmt.Errorf("make EvGetURLBySessionID fail: %w", err)
	}

	res, err := s.repo.On(ctx, e)
	if err != nil {
		return nil, fmt.Errorf("on EvGetURLBySessionID fail: %w", err)
	}

	p, err := event.GetPayload[event.PayloadGetURLBySessionID](res)
	if err != nil {
		return nil, fmt.Errorf("fetch PayloadGetURLBySessionID fail: %w", err)
	}

	// Конвертируем ShortPath в ShortURL
	u := s.shortBaseURL.URL
	for i := range p.Result {
		u.Path = p.Result[i].ShortURL
		p.Result[i].ShortURL = u.String()
	}

	if len(p.Result) == 0 {
		return &p, ErrNoContent
	}

	return &p, nil
}

func (s *Service) AddURL(ctx context.Context, originalURL string) (string, error) {
	// Нормализация URL
	normalizedURL, err := s.NormalizeURL(ctx, originalURL)
	if err != nil {
		return "", fmt.Errorf("cannot normalize long URL: %w", err)
	}

	// Предохранитель от циклических сокращений
	if s.shortBaseURL.URL.Hostname() == normalizedURL.Hostname() {
		return "", ErrDomainIsBlocked
	}

	// Запись URL в БД
	var e event.Event
	var session model.Session
	var sessionMode bool
	if session, sessionMode = model.GetSession(ctx, s.keySession); sessionMode {
		e, err = event.MakeEvent(
			event.PayloadAddURLBySessionID{
				OriginalURL: normalizedURL.String(),
				SessionID:   session.ID,
			},
			nil,
		)
	} else {
		e, err = event.MakeEvent(
			event.PayloadAddURL{
				OriginalURL: normalizedURL.String(),
			},
			nil,
		)
	}
	if err != nil {
		return "", fmt.Errorf("failed to persist data: %w", err)
	}

	resEvent, err := s.repo.On(ctx, e)
	if err != nil {
		return "", fmt.Errorf("failed to persist data: %w", err)
	}

	u := s.shortBaseURL.URL
	var conflictFlag bool
	if sessionMode {
		p, err := event.GetPayload[event.PayloadAddURLBySessionID](resEvent)
		if err != nil {
			return "", fmt.Errorf("failed to persist data: %w", err)
		}
		u.Path = p.ShortURL
		conflictFlag = p.ConflictFlag
	} else {
		p, err := event.GetPayload[event.PayloadAddURL](resEvent)
		if err != nil {
			return "", fmt.Errorf("failed to persist data: %w", err)
		}
		u.Path = p.ShortURL
		conflictFlag = p.ConflictFlag
	}

	slog.Info("service.AddURL",
		slog.String("original_url", originalURL),
		slog.String("short_url", u.String()),
		slog.Bool("conflict_flag", conflictFlag),
	)

	if conflictFlag {
		return u.String(), fmt.Errorf("original URL %s already shortened to short URL %s: %w", normalizedURL, u.String(), ErrConflict)
	}

	return u.String(), nil
}

func (s *Service) GetURL(ctx context.Context, sURL string) (string, error) {
	e, err := event.MakeEvent(event.PayloadGetURL{ShortURL: sURL}, nil)
	if err != nil {
		return "", fmt.Errorf("make event EvGetURL fail: %w", err)
	}

	res, err := s.repo.On(ctx, e)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve data: %w", err)
	}

	p, err := event.GetPayload[event.PayloadGetURL](res)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	if p.IsGone {
		return p.OriginalURL, ErrGone
	}

	return p.OriginalURL, nil
}

func (s *Service) Ping(ctx context.Context) error {
	return s.repo.Ping(ctx)
}

// Batch получает на вход event.PayloadBatch и возращает готовое event.PayloadBatch
func (s *Service) Batch(ctx context.Context, p event.PayloadBatch) (event.PayloadBatch, error) {

	// Нормализация OriginalURL
	for i := range p.Batch {
		normalizedURL, err := s.NormalizeURL(ctx, p.Batch[i].OriginalURL)
		if err != nil {
			p.Batch[i].Err = ErrInvalidURLFormat.Error()
		} else {
			p.Batch[i].OriginalURL = normalizedURL.String()
		}
	}

	e, err := event.MakeEvent(p, nil)
	if err != nil {
		return p, err
	}

	// Записываем в хранилище
	resEvent, err := s.repo.On(ctx, e)
	if err != nil {
		return p, err
	}

	resPayload, err := event.GetPayload[event.PayloadBatch](resEvent)
	if err != nil {
		return resPayload, err
	}

	// Генерируем ShortURL
	for i := range resPayload.Batch {
		// Генерируем короткий URL
		sURL := s.shortBaseURL.URL
		sURL.Path = resPayload.Batch[i].ShortURL

		resPayload.Batch[i].ShortURL = sURL.String()
		resPayload.Batch[i].OriginalURL = ""
	}

	return resPayload, nil
}
