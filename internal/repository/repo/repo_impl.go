package repo

import (
	"context"
	"errors"
	"fmt"
	"murl/internal/model/event"
	"murl/internal/repository"
	"murl/internal/repository/inmem"
	"murl/internal/repository/pg"
	"murl/internal/repository/pgc"
	"murl/internal/repository/pgc/pgcluster"
	"murl/internal/repository/wal"
)

//go:generate $GOPATH/bin/mockgen -source=../pgc/instance/pg_instance_impl.go -destination=repo_impl_pg_instance_impl_mock_test.go -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../wal/wal.go                       -destination=repo_impl_wal_mock_test.go              -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../pgc/pg_cluster.go                -destination=repo_impl_pg_cluster_mock_test.go       -package=$GOPACKAGE
//go:generate $GOPATH/bin/mockgen -source=../repo_links.go                    -destination=repo_impl_repo_links_mock_test.go       -package=$GOPACKAGE

const (
	RepoInMemory string = "InMemory"
	RepoPostgres string = "PgDB"
)

type repo struct {
	pgCluster            pgc.PgCluster
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
		r.pgCluster, err = pgcluster.NewPgCluster(ctx, cfg, nil)
		if err != nil {
			return nil, err
		}
		if err := r.pgCluster.RunMigrations(ctx); err != nil {
			return nil, fmt.Errorf("run migrations fail: %w", err)
		}
		r.repoLinks = pg.NewPgRepoLinks(r.pgCluster)
		r.repoLinksBySessionID = pg.NewPgRepoLinksBySessionID(r.pgCluster)
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

func (r *repo) Close() error {
	r.wal.Close()

	if r.pgCluster == nil {
		return nil
	}

	return r.pgCluster.Close()
}

func (r *repo) Ping(ctx context.Context) error {
	if r.pgCluster == nil {
		return nil
	}

	return r.pgCluster.Ping(ctx)
}

func (r *repo) On(ctx context.Context, e event.Event) (event.Event, error) {
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
	if r.pgCluster == nil {
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
