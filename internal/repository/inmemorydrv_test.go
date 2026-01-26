package repository

import (
	"murl/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpSert(t *testing.T) {
	// Конфигурация
	cfg, err := config.GetConfig(nil, nil)
	assert.Nil(t, err)
	drv := NewInMemoryDrv(cfg)

	testPlan := []struct {
		name      string
		upsertStr string
		shardID   byte
		idx       uint64
	}{
		{name: "First upsert", upsertStr: "123", shardID: 0, idx: 0},
		{name: "Second upsert", upsertStr: "1234", shardID: 0, idx: 1},
		{name: "2nd First upsert", upsertStr: "123", shardID: 0, idx: 0},
		{name: "3rd upsert", upsertStr: "", shardID: 0, idx: 2},
	}

	for _, test := range testPlan {
		t.Run(test.name, func(t *testing.T) {
			shardID, idx, err := drv.UpSert(test.upsertStr)
			assert.NoError(t, err)
			assert.Equal(t, test.shardID, shardID)
			assert.Equal(t, test.idx, idx)

			val, err := drv.Select(shardID, idx)
			assert.NoError(t, err)
			assert.Equal(t, test.upsertStr, val)
		})
	}
}

func TestSelect(t *testing.T) {
	// Конфигурация
	cfg, err := config.GetConfig(nil, nil)
	assert.Nil(t, err)
	drv := NewInMemoryDrv(cfg)

	testPlan := []struct {
		name string
		sID  byte
		idx  uint64
		err  error
	}{
		{name: "Not found", sID: 1, idx: 0, err: ErrDBRecordNotFound},
	}

	for _, test := range testPlan {
		t.Run(test.name, func(t *testing.T) {
			_, err := drv.Select(test.sID, test.idx)
			assert.ErrorIs(t, err, test.err)
		})
	}
}
