package pg

import (
	"context"
	"fmt"
	"murl/internal/adapters/pgc"
	"murl/internal/dto"
	"murl/internal/repository"
)

// StatResult описывает результат выполнения.
type StatsResult struct {
	Users uint64
	URLs  uint64
}

// sqlSsqlRepoStatstats — запрос, возвращающий статистику
var sqlRepoStats = pgc.NewQuery(`
	SELECT COUNT(DISTINCT session_id) AS users, COUNT(url) urls
	FROM murl
	WHERE deleted = 'f'::boolean
	LIMIT 1;
`,
	func(u *StatsResult) []any {
		return []any{&u.Users, &u.URLs}
	},
).AsRead()

// PgStat реализует интерфейс repository.RepoStat для PostgreSQL.
type PgRepoStats struct {
	inst pgc.PgInstance
}

// NewPgRepoStat создает новый экземпляр репозитория статистики
func NewPgRepoStats(inst pgc.PgInstance) repository.RepoStats {
	return &PgRepoStats{
		inst: inst,
	}
}

// UpSert записывает URL в БД с привязкой к сессии и возвращает короткий путь.
func (s *PgRepoStats) GetStats(ctx context.Context) (dto.Stats, error) {
	res, err := pgc.FetchRow(ctx, s.inst, sqlRepoStats)
	if err != nil {
		return dto.Stats{}, fmt.Errorf("failed to execute query (%s): %w", sqlRepoStats.Name(), err)
	}

	return dto.Stats{
		URLs:  res.URLs,
		Users: res.Users,
	}, nil
}
