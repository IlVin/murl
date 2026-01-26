package repository

import (
	"murl/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
)

type plan struct {
	name string
	arg  string
	sID  byte
	idx  uint64
}

// Записываем длинные строки в хранилище и получаем строки-идентификаторы с закодированным на 1й позиции номером шарда
func TestStoreSaveLoad(t *testing.T) {
	cfg, err := config.GetConfig(nil, nil)
	assert.Nil(t, err)
	drv := NewInMemoryDrv(cfg)
	store := NewStore(cfg, drv)

	testPlan := []plan{
		{name: "str1", arg: "qwe werqwe rqwe r", sID: 0, idx: 0},
		{name: "str2", arg: "qwe", sID: 0, idx: 1},
		{name: "str3", arg: "erqwer qwer qw", sID: 0, idx: 2},
		{name: "str4", arg: "ssdfsdf", sID: 0, idx: 3},
		{name: "str5", arg: "qwe", sID: 0, idx: 1},
	}

	for _, p := range testPlan {
		t.Run(p.name, func(t *testing.T) {
			sID, idx, err := store.Save(p.arg)
			assert.NoError(t, err)
			assert.Equal(t, sID, p.sID)
			assert.Equal(t, idx, p.idx)

			str, err := store.Load(sID, idx)
			assert.NoError(t, err)
			assert.Equal(t, str, p.arg)
		})
	}

}
