package inmem

import (
	"context"
	"murl/internal/dto"
	"murl/internal/repository"
)

type InMemRepoStats struct {
	c *InMemCore
}

func NewInMemRepoStats(core *InMemCore) repository.RepoStats {
	r := &InMemRepoStats{
		c: core,
	}
	return r
}

// UpSert записывает originalURL строку в шард БД и возвращает shortPath строку, признак конфликта и ошибку
func (r *InMemRepoStats) GetStats(ctx context.Context) (dto.Stats, error) {

	var Users uint64 = 0
	var URLs uint64 = 0
	for shardID := range int(r.c.Size()) {
		shard, err := r.c.GetShard(byte(shardID))
		if err != nil {
			return dto.Stats{}, err
		}
		shard.Mu.RLock()
		URLs += uint64(len(shard.Data))
		Users += uint64(len(shard.Sessions))
		shard.Mu.RUnlock()
	}

	return dto.Stats{
		URLs:  URLs,
		Users: Users,
	}, nil
}
