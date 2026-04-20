package repo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"murl/internal/adapters/pgc"
	"murl/internal/model/event"
	"murl/internal/repository"
	"murl/internal/repository/inmem"
	"murl/internal/repository/pg"
	"murl/internal/repository/wal"
)

//go:generate $GOPATH/bin/mockgen -source=../../adapters/pgc/pgc.go -destination=repo_impl_pgc_mock_test.go          -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../wal/wal.go             -destination=repo_impl_wal_mock_test.go          -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../repo_links.go          -destination=repo_impl_repo_links_mock_test.go   -package=$GOPACKAGE

const (
	RepoInMemory string = "InMemory"
	RepoPostgres string = "PgDB"
)

type repo struct {
	pgInst               pgc.PgInstance
	memCore              *inmem.InMemCore
	repoLinksBySessionID repository.RepoLinksBySessionID
	repoLinks            repository.RepoLinks
	wal                  wal.WAL
}

// Конструктор хранилища с драйвером
func NewRepo(ctx context.Context, cfg repository.RepoConfig) (r *repo, err error) {

	// Инициализация бизнес репозиториев
	r = &repo{}

	if cfg.RepoDrv() == RepoPostgres {
		r.pgInst, err = pgc.NewPgConnector(ctx, cfg.DBDSN())
		if err != nil {
			return nil, err
		}
		if err := r.pgInst.RunMigrations(ctx); err != nil {
			return nil, fmt.Errorf("run migrations fail: %w", err)
		}
		r.repoLinks = pg.NewPgRepoLinks(r.pgInst)
		r.repoLinksBySessionID = pg.NewPgRepoLinksBySessionID(r.pgInst)
	} else if cfg.RepoDrv() == RepoInMemory {
		r.memCore, err = inmem.NewInMemCore(cfg)
		if err != nil {
			return nil, err
		}
		r.repoLinks = inmem.NewInMemRepoLinks(r.memCore)
		r.repoLinksBySessionID = inmem.NewInMemRepoLinksBySessionID(r.memCore)
	} else {
		return nil, fmt.Errorf("unknown repo driver '%s': %w", cfg.RepoDrv(), repository.ErrInternalServerError)
	}

	if cfg.EventStoragePath() != "" {
		// Загрузка событий из WAL
		slog.Info("Load Events from WAL",
			slog.String("path", cfg.EventStoragePath()),
		)
		walCtx := context.Background()
		ch, err := wal.LoadWAL(walCtx, cfg.EventStoragePath())
		if err != nil {
			return nil, err
		}
		for e := range ch {
			if _, err := r.On(walCtx, e); err != nil {
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

func (r *repo) Close(ctx context.Context) error {
	r.wal.Close()

	if r.pgInst == nil {
		return nil
	}

	return r.pgInst.Close(ctx)
}

func (r *repo) Ping(ctx context.Context) error {
	if r.pgInst == nil {
		return nil
	}

	return r.pgInst.Ping(ctx)
}

func (r *repo) On(ctx context.Context, e event.Event) (event.Event, error) {
	slog.Info("Event",
		slog.String("e.Type", e.GetType().String()),
	)
	switch e.GetType() {
	case event.EvAddURL:
		return r.evAddURL(ctx, e)
	case event.EvAddURLBySessionID:
		return r.evAddURLBySessionID(ctx, e)
	case event.EvDeleteURLBySessionID:
		return r.evDeleteURLBySessionID(ctx, e)
	case event.EvGetURL:
		return r.evGetURL(ctx, e)
	case event.EvGetURLBySessionID:
		return r.evGetURLBySessionID(ctx, e)
	case event.EvBatch:
		return r.evBatch(ctx, e)

	}
	return nil, fmt.Errorf("event type '%s' not implemented", e.GetType())
}

// evAddURL добавляет URL в БД
func (r *repo) evAddURL(ctx context.Context, e event.Event) (resEvent event.Event, err error) {
	// Парсинг события и проверка типа события
	p, err := event.GetPayload[event.PayloadAddURL](e)
	if err != nil {
		return nil, err
	}

	// Запись в БД маппинга
	if len(p.ShortURL) > 0 {
		if err := r.repoLinks.Set(ctx, p.OriginalURL, p.ShortURL); err != nil {
			return nil, err
		}
		return e, nil
	} else {
		shortPath, conflictFlag, err := r.repoLinks.UpSert(ctx, p.OriginalURL)
		if err != nil {
			return nil, err
		}

		// Приведение Path к URL
		p.ShortURL = shortPath
		p.ConflictFlag = conflictFlag

		// Добавляем событие в WAL
		resEvent, err = event.MakeEvent(p, e)
		if err != nil {
			return nil, err
		}
		if r.wal != nil && !p.ConflictFlag {
			if err := r.wal.Push(resEvent); err != nil {
				return nil, err
			}
		}
	}

	return resEvent, nil
}

// evAddURL добавляет URL в БД шардируя по SessionID
func (r *repo) evAddURLBySessionID(ctx context.Context, e event.Event) (resEvent event.Event, err error) {
	// Парсинг события и проверка типа события
	p, err := event.GetPayload[event.PayloadAddURLBySessionID](e)
	if err != nil {
		return nil, err
	}

	// Запись в БД маппинга
	if len(p.ShortURL) > 0 {
		if err := r.repoLinksBySessionID.Set(ctx, p.SessionID.String(), p.OriginalURL, p.ShortURL); err != nil {
			return nil, err
		}
		return e, nil
	} else {
		shortPath, conflictFlag, err := r.repoLinksBySessionID.UpSert(ctx, p.SessionID.String(), p.OriginalURL)
		if err != nil {
			return nil, err
		}

		// Приведение Path к URL
		p.ShortURL = shortPath
		p.ConflictFlag = conflictFlag

		// Добавляем событие в WAL
		resEvent, err = event.MakeEvent(p, e)
		if err != nil {
			return nil, err
		}
		if r.wal != nil && !p.ConflictFlag {
			if err := r.wal.Push(resEvent); err != nil {
				return nil, err
			}
		}
	}

	return resEvent, nil
}

// evDeleteURL добавляет URL в БД шардируя по SessionID
func (r *repo) evDeleteURLBySessionID(ctx context.Context, e event.Event) (resEvent event.Event, err error) {
	// Парсинг события и проверка типа события
	p, err := event.GetPayload[event.PayloadDeleteURLBySessionID](e)
	if err != nil {
		return nil, err
	}

	// Отложенное удаление реализовано только для Pg
	if r.pgInst == nil {
		return nil, errors.New("DeleteURLBySessionID is not implemented")
	}

	if err := r.repoLinksBySessionID.BatchDelBySessionID(ctx, p.SessionID.String(), p); err != nil {
		return nil, err
	}

	return nil, nil
}

// evGetURL считывает OriginURL по ShortPath из БД
func (r *repo) evGetURL(ctx context.Context, e event.Event) (event.Event, error) {
	// Парсинг события и проверка типа события
	p, err := event.GetPayload[event.PayloadGetURL](e)
	if err != nil {
		return nil, err
	}

	originalURL, deleted, err := r.repoLinks.Select(ctx, p.ShortURL)
	if err != nil {
		return nil, err
	}

	p.OriginalURL = originalURL
	p.IsGone = deleted

	return event.MakeEvent(p, e)
}

// evGetURL считывает OriginURL по ShortPath из БД
func (r *repo) evGetURLBySessionID(ctx context.Context, e event.Event) (event.Event, error) {
	// Парсинг события и проверка типа события
	p, err := event.GetPayload[event.PayloadGetURLBySessionID](e)
	if err != nil {
		return nil, err
	}

	items, err := r.repoLinksBySessionID.SelectAll(ctx, p.SessionID.String())
	if err != nil {
		return nil, err
	}

	p.Result = items

	return event.MakeEvent(p, e)
}

// evBatch добавляет пакет URL в БД
func (r *repo) evBatch(ctx context.Context, e event.Event) (event.Event, error) {
	// Парсинг события и проверка типа события
	p, err := event.GetPayload[event.PayloadBatch](e)
	if err != nil {
		return nil, err
	}

	res := r.repoLinks.BatchUpSert(ctx, p)

	// Добавляем событие в WAL
	resEvent, err := event.MakeEvent(res, e)
	if err != nil {
		return nil, err
	}
	if r.wal != nil {
		if err := r.wal.Push(resEvent); err != nil {
			return nil, err
		}
	}

	return resEvent, nil
}
