// Package service содержит реализацию прикладного слоя приложения (Use Cases).
// Сервис инкапсулирует правила валидации, нормализации URL и координацию
// работы между хранилищем и системой аудита.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/config"
	"murl/internal/domain"
	"murl/internal/dto"
	"murl/internal/model"
	"murl/internal/repository"
	"net/url"
	"time"

	"github.com/google/uuid"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=service_mock_test.go -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../repository/repo.go -destination=service_repo_mock_test.go -package=$GOPACKAGE

var (
	// ErrInvalidURLFormat возвращается, если предоставленная строка не является корректным URL.
	ErrInvalidURLFormat = errors.New("invalid URL format")
	// ErrDomainIsBlocked возвращается при попытке сократить URL, указывающий на сам сервис (защита от циклов).
	ErrDomainIsBlocked = errors.New("domain is blocked")
	// ErrConflict сигнализирует о том, что данный URL уже был сокращен ранее (используется для статуса 409).
	ErrConflict = errors.New("URL is already shortened")
	// ErrQuotaReached возвращается при превышении лимитов пользователя.
	ErrQuotaReached = errors.New("quota reached")
	// ErrNoContent возвращается, если запрос не вернул данных.
	ErrNoContent = errors.New("no content")
	// ErrGone возвращается, если ссылка была удалена владельцем (статус 410).
	ErrGone = errors.New("gone")
)

// ServiceConfig определяет набор параметров конфигурации, необходимых для работы сервиса.
type ServiceConfig interface {
	ShortBaseURL() config.ShortBaseURL
	KeySession() config.KeySession
}

// AuditlogNotifier описывает интерфейс для отправки асинхронных уведомлений аудита.
type AuditlogNotifier interface {
	Notify(ctx context.Context, n domain.Notification) error
}

// Service — основной компонент бизнес-логики приложения.
type Service struct {
	shortBaseURL config.ShortBaseURL
	repo         repository.Repo
	aNotifier    AuditlogNotifier
	keySession   config.KeySession
}

// NewService создает новый экземпляр бизнес-сервиса.
func NewService(ctx context.Context, cfg ServiceConfig, repo repository.Repo, an AuditlogNotifier) *Service {
	return &Service{
		shortBaseURL: cfg.ShortBaseURL(),
		repo:         repo,
		aNotifier:    an,
		keySession:   cfg.KeySession(),
	}
}

// NormalizeURL выполняет парсинг и валидацию входящего URL.
// Проверяет наличие схемы (http/https) и абсолютность пути.
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

// DeleteURLBySessionID инициирует процесс массового удаления ссылок пользователя.
func (s *Service) DeleteURLBySessionID(ctx context.Context, session model.Session, data []string) error {
	shortURLs := make([]string, 0, len(data))
	for i := range data {
		shortURLs = append(shortURLs, "/"+data[i])
	}

	slog.Info("service.DeleteURLBySessionID",
		slog.Any("sessionID", session.ID),
		slog.Any("shortURLs", shortURLs),
	)

	err := s.repo.DeleteURLBySessionID(
		ctx,
		dto.DeleteURLBySessionID{
			SessionID: session.ID,
			ShortURLs: shortURLs,
		})
	if err != nil {
		return fmt.Errorf("repo.DeleteURLBySessionID fail: %w", err)
	}

	return nil
}

// GetURLBySessionID возвращает список всех сокращенных ссылок пользователя,
// преобразуя внутренние пути в полные URL.
func (s *Service) GetURLBySessionID(ctx context.Context, session model.Session) (dto.GetURLBySessionID, error) {
	res, err := s.repo.GetURLBySessionID(ctx, dto.GetURLBySessionID{SessionID: session.ID})
	if err != nil {
		return res, fmt.Errorf("repo.GetURLBySessionID fail: %w", err)
	}

	// Конвертируем ShortPath в ShortURL
	u := s.shortBaseURL.URL
	for i := range res.Result {
		u.Path = res.Result[i].ShortURL
		res.Result[i].ShortURL = u.String()
	}

	if len(res.Result) == 0 {
		return res, ErrNoContent
	}

	return res, nil
}

// AddURL сокращает переданный URL. Автоматически определяет контекст (анонимно/сессия),
// проверяет дубликаты и отправляет событие "shorten" в систему аудита.
func (s *Service) AddURL(ctx context.Context, originalURL string) (string, error) {
	// Нормализация URL
	normalizedURL, err := s.NormalizeURL(ctx, originalURL)
	if err != nil {
		return "", fmt.Errorf("cannot normalize long URL: %w", err)
	}

	// Предохранитель от циклических сокращений
	if s.shortBaseURL.Hostname() == normalizedURL.Hostname() {
		return "", ErrDomainIsBlocked
	}

	u := s.shortBaseURL.URL
	var conflictFlag bool
	var session model.Session
	var sessionMode bool

	if session, sessionMode = model.GetSession(ctx, s.keySession); sessionMode {
		res, repoErr := s.repo.AddURLBySessionID(
			ctx,
			dto.AddURLBySessionID{
				AddURL: dto.AddURL{
					OriginalURL: normalizedURL.String(),
				},
				SessionID: session.ID,
			})
		if repoErr != nil {
			return "", fmt.Errorf("failed to persist data: %w", repoErr)
		}
		u.Path = res.ShortURL
		conflictFlag = res.ConflictFlag
	} else {
		res, repoErr := s.repo.AddURL(
			ctx,
			dto.AddURL{
				OriginalURL: normalizedURL.String(),
			})
		if repoErr != nil {
			return "", fmt.Errorf("failed to persist data: %w", repoErr)
		}
		u.Path = res.ShortURL
		conflictFlag = res.ConflictFlag
	}

	// Отправляем нотификацию
	notifCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	errNotif := s.sendNotification(notifCtx, "shorten", &session.ID, originalURL)
	if errNotif != nil {
		slog.Error("audit log notification failure",
			slog.Any("err", errNotif),
		)
	}

	if conflictFlag {
		return u.String(), fmt.Errorf("original URL %s already shortened to short URL %s: %w", normalizedURL, u.String(), ErrConflict)
	}

	return u.String(), nil
}

func (s *Service) sendNotification(ctx context.Context, action string, userID *uuid.UUID, originalURL string) error {
	notif := dto.AuditlogNotification{
		UnixTimestamp: time.Now().Unix(), // unix timestamp события
		Action:        "shorten",         // действие: shorten (создание) или follow (прохождение по ссылке)
		OrigURL:       originalURL,
		UserID:        userID,
	}

	n, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("audit notification marshalling failure: %w", err)
	}
	if err := s.aNotifier.Notify(ctx, domain.Notification{Message: n}); err != nil {
		return fmt.Errorf("audit log notification send failure: %w", err)
	}
	return nil
}

// GetURL возвращает оригинальный URL по короткому идентификатору.
// При каждом успешном запросе отправляет событие "follow" в аудит.
func (s *Service) GetURL(ctx context.Context, sURL string) (string, error) {
	res, err := s.repo.GetURL(
		ctx,
		dto.GetURL{
			ShortURL: sURL,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve data: %w", err)
	}

	if res.IsGone {
		return res.OriginalURL, ErrGone
	}

	// Отправляем нотификацию
	notifCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	errNotif := s.sendNotification(notifCtx, "follow", nil, res.OriginalURL)
	if errNotif != nil {
		slog.Error("audit log notification failure",
			slog.Any("err", err),
		)
	}

	return res.OriginalURL, nil
}

// Ping проверяет работоспособность нижележащего хранилища.
func (s *Service) Ping(ctx context.Context) error {
	return s.repo.Ping(ctx)
}

// Batch выполняет пакетное сокращение списка URL.
// Ошибки нормализации конкретных строк записываются в поле Err соответствующих BatchItem.
func (s *Service) Batch(ctx context.Context, p dto.Batch) (dto.Batch, error) {

	// Нормализация OriginalURL
	for i := range p.Batch {
		normalizedURL, err := s.NormalizeURL(ctx, p.Batch[i].OriginalURL)
		if err != nil {
			p.Batch[i].Err = ErrInvalidURLFormat.Error()
		} else {
			p.Batch[i].OriginalURL = normalizedURL.String()
		}
	}

	// Записываем в хранилище
	res, err := s.repo.Batch(ctx, p)
	if err != nil {
		return p, err
	}

	// Генерируем ShortURL
	for i := range res.Batch {
		// Генерируем короткий URL
		sURL := s.shortBaseURL.URL
		sURL.Path = res.Batch[i].ShortURL

		res.Batch[i].ShortURL = sURL.String()
		res.Batch[i].OriginalURL = ""
	}

	return res, nil
}
