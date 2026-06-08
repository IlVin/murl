// Package repo предоставляет конкретную реализацию интерфейса repository.Repo.
// Модуль отвечает за инициализацию выбранного драйвера хранилища, управление
// журналом WAL и оркестрацию запросов между специализированными репозиториями.
package repo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/adapters/pgc"
	"murl/internal/dto"
	"murl/internal/model/event"
	"murl/internal/repository"
	"murl/internal/repository/inmem"
	"murl/internal/repository/pg"
	"murl/internal/repository/wal"
)

//go:generate $GOPATH/bin/mockgen -source=../../adapters/pgc/pgc.go       -destination=repo_impl_pgc_mock_test.go                       -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../wal/wal.go                   -destination=repo_impl_wal_mock_test.go                       -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../repo_links.go                -destination=repo_impl_repo_links_mock_test.go                -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../repo_links_by_session_id.go  -destination=repo_impl_repo_links_by_session_id_mock_test.go  -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../repo_stats.go                -destination=repo_impl_repo_stats_mock_test.go                -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../../model/event/event.go      -destination=repo_impl_event_mock_test.go                     -package=$GOPACKAGE

const (
	// RepoInMemory — идентификатор драйвера для работы в оперативной памяти.
	RepoInMemory string = "InMemory"
	// RepoPostgres — идентификатор драйвера для работы с PostgreSQL.
	RepoPostgres string = "PgDB"
)

// repo — внутренняя структура, объединяющая все компоненты хранилища.
type repo struct {
	pgInst               pgc.PgInstance
	memCore              *inmem.InMemCore
	repoLinksBySessionID repository.RepoLinksBySessionID
	repoLinks            repository.RepoLinks
	repoStats            repository.RepoStats
	wal                  wal.WAL
}

// NewRepo — конструктор универсального репозитория.
// Выполняет инициализацию выбранного драйвера, запускает миграции для БД
// и производит восстановление состояния (replay) из WAL, если указан путь к файлу событий.
func NewRepo(ctx context.Context, cfg repository.RepoConfig) (r *repo, err error) {

	// Инициализация бизнес репозиториев
	r = &repo{}

	if cfg.RepoDrv() == RepoPostgres {
		r.pgInst, err = pgc.NewPgConnector(ctx, cfg.DBDSN())
		r.pgInst.WithSlogHandler(slog.Default().Handler())
		if err != nil {
			return nil, err
		}
		if err = r.pgInst.RunMigrations(ctx); err != nil {
			return nil, fmt.Errorf("run migrations fail: %w", err)
		}
		r.repoLinks = pg.NewPgRepoLinks(r.pgInst)
		r.repoLinksBySessionID = pg.NewPgRepoLinksBySessionID(r.pgInst)
		r.repoStats = pg.NewPgRepoStats(r.pgInst)
	} else if cfg.RepoDrv() == RepoInMemory {
		r.memCore, err = inmem.NewInMemCore(cfg)
		if err != nil {
			return nil, err
		}
		r.repoLinks = inmem.NewInMemRepoLinks(r.memCore)
		r.repoLinksBySessionID = inmem.NewInMemRepoLinksBySessionID(r.memCore)
		r.repoStats = inmem.NewInMemRepoStats(r.memCore)
	} else {
		return nil, fmt.Errorf("unknown repo driver '%s': %w", cfg.RepoDrv(), repository.ErrInternalServerError)
	}

	if cfg.EventStoragePath() != "" {
		// Загрузка событий из WAL
		slog.Info("Load Events from WAL",
			slog.String("path", cfg.EventStoragePath()),
		)
		walCtx := context.Background()
		ch, errWAL := wal.LoadWAL(walCtx, cfg.EventStoragePath())
		if errWAL != nil {
			return nil, errWAL
		}
		for e := range ch {
			if err = r.On(walCtx, e); err != nil {
				return r, fmt.Errorf("wal load fail: %w", err)
			}
		}

		r.wal, err = wal.NewWAL(cfg.EventStoragePath())
		if err != nil {
			return nil, fmt.Errorf("cannot open event storage file: %w", err)
		}
	}

	return r, nil
}

// Close корректно завершает работу всех компонентов репозитория.
func (r *repo) Close(ctx context.Context) error {
	if r.wal != nil {
		if err := r.wal.Close(); err != nil {
			slog.Error("wal close fail",
				slog.Any("err", err),
			)
		}
	}

	if r.pgInst == nil {
		return nil
	}

	return r.pgInst.Close(ctx)
}

// Ping проверяет доступность внешнего хранилища (если используется Postgres).
func (r *repo) Ping(ctx context.Context) error {
	if r.pgInst == nil {
		return nil
	}

	return r.pgInst.Ping(ctx)
}

// pushToWAL Добавляем событие в WAL
func (r *repo) pushToWAL(e event.Event, err error) (event.Event, error) {
	if err != nil {
		return nil, err
	}
	if err := r.wal.Push(e); err != nil {
		return nil, err
	}
	return e, nil
}

// On применяет входящее событие к текущему состоянию репозитория.
// Используется для восстановления данных из WAL при старте приложения.
func (r *repo) On(ctx context.Context, e event.Event) error {
	switch e.GetType() {
	case dto.EvType("EvAddURL"):
		p, err := event.GetPayload[dto.AddURL](e)
		if err != nil {
			return err
		}
		_, err = r.AddURL(ctx, p)
		if err != nil {
			return err
		}
		return nil
	case dto.EvType("EvAddURLBySessionID"):
		p, err := event.GetPayload[dto.AddURLBySessionID](e)
		if err != nil {
			return err
		}
		_, err = r.AddURLBySessionID(ctx, p)
		if err != nil {
			return err
		}
		return nil
	case dto.EvType("EvDeleteURLBySessionID"):
		p, err := event.GetPayload[dto.DeleteURLBySessionID](e)
		if err != nil {
			return err
		}
		err = r.DeleteURLBySessionID(ctx, p)
		if err != nil {
			return err
		}
		return nil
	case dto.EvType("EvBatch"):
		p, err := event.GetPayload[dto.Batch](e)
		if err != nil {
			return err
		}
		_, err = r.Batch(ctx, p)
		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("event type '%s' not implemented", e.GetType())
}

// GetInternalStats возвращает статистику
func (r *repo) GetInternalStats(ctx context.Context) (dto.Stats, error) {
	return r.repoStats.GetStats(ctx)
}

// AddURL сохраняет новую ссылку. Если WAL активен и конфликта не возникло,
// событие добавления логируется на диск.
func (r *repo) AddURL(ctx context.Context, p dto.AddURL) (dto.AddURL, error) {
	// Запись в БД маппинга
	if len(p.ShortURL) > 0 {
		if err := r.repoLinks.Set(ctx, p.OriginalURL, p.ShortURL); err != nil {
			return p, err
		}
		return p, nil
	} else {
		shortPath, conflictFlag, err := r.repoLinks.UpSert(ctx, p.OriginalURL)
		if err != nil {
			return p, err
		}

		// Приведение Path к URL
		p.ShortURL = shortPath
		p.ConflictFlag = conflictFlag
	}
	if r.wal != nil && !p.ConflictFlag {
		if _, err := r.pushToWAL(event.MakeEvent(p, nil)); err != nil {
			return p, err
		}
	}
	return p, nil
}

// AddURLBySessionID сохраняет ссылку с привязкой к сессии пользователя.
func (r *repo) AddURLBySessionID(ctx context.Context, p dto.AddURLBySessionID) (dto.AddURLBySessionID, error) {
	// Запись в БД маппинга
	if len(p.ShortURL) > 0 {
		if err := r.repoLinksBySessionID.Set(ctx, p.SessionID.String(), p.OriginalURL, p.ShortURL); err != nil {
			return p, err
		}
		return p, nil
	} else {
		shortPath, conflictFlag, err := r.repoLinksBySessionID.UpSert(ctx, p.SessionID.String(), p.OriginalURL)
		if err != nil {
			return p, err
		}

		// Приведение Path к URL
		p.ShortURL = shortPath
		p.ConflictFlag = conflictFlag

		if r.wal != nil && !p.ConflictFlag {
			if _, err := r.pushToWAL(event.MakeEvent(p, nil)); err != nil {
				return p, err
			}
		}
	}

	return p, nil
}

// GetURL извлекает оригинальный URL по короткому пути.
func (r *repo) GetURL(ctx context.Context, p dto.GetURL) (dto.GetURL, error) {
	originalURL, deleted, err := r.repoLinks.Select(ctx, p.ShortURL)
	if err != nil {
		return p, err
	}

	p.OriginalURL = originalURL
	p.IsGone = deleted

	return p, nil
}

// GetURLBySessionID возвращает список всех ссылок конкретной сессии.
func (r *repo) GetURLBySessionID(ctx context.Context, p dto.GetURLBySessionID) (dto.GetURLBySessionID, error) {
	items, err := r.repoLinksBySessionID.SelectAll(ctx, p.SessionID.String())
	if err != nil {
		return p, err
	}

	p.Result = items

	return p, nil
}

// DeleteURLBySessionID помечает ссылки пользователя как удаленные.
// На текущий момент полноценно реализовано только для драйвера Postgres.
func (r *repo) DeleteURLBySessionID(ctx context.Context, p dto.DeleteURLBySessionID) error {
	// Отложенное удаление реализовано только для Pg
	if r.pgInst == nil {
		return errors.New("DeleteURLBySessionID is not implemented")
	}

	if err := r.repoLinksBySessionID.BatchDelBySessionID(ctx, p.SessionID.String(), p); err != nil {
		return err
	}

	if r.wal != nil {
		if _, err := r.pushToWAL(event.MakeEvent(p, nil)); err != nil {
			return err
		}
	}

	return nil
}

// Batch выполняет пакетную вставку ссылок и логирует операцию в WAL.
func (r *repo) Batch(ctx context.Context, p dto.Batch) (dto.Batch, error) {
	res := r.repoLinks.BatchUpSert(ctx, p)

	if r.wal != nil {
		if _, err := r.pushToWAL(event.MakeEvent(p, nil)); err != nil {
			return p, err
		}
	}

	return res, nil
}
