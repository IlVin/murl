package repository

import (
	"math"
	"murl/internal/config"
	"testing"
)

type plan struct {
	name string
	idx  int
	arg  string
	res  string
}

func TestStoreIdx2Str(t *testing.T) {
	// Конфигурация
	cfg := config.GetConfig()
	// Адаптер к определенной БД. Оперируем: вычислить шард, записать строку, прочитать до цифровому ID
	drv := NewInMemoryDrv(cfg)
	// Схема хранилища: Записать строку в БД и получить строковый идентификатор этой записи
	store := NewStore(cfg, drv)

	testPlan := []plan{
		{name: "0", idx: 0, res: "AA"},
		{name: "1", idx: 1, res: "Ag"},
		{name: "2", idx: 2, res: "BA"},
		{name: "-5", idx: -5, res: "CQ"},
		{name: "MaxInt", idx: math.MaxInt, res: "_v__________AQ"},
		{name: "MinInt", idx: math.MinInt, res: "____________AQ"},
	}

	for _, p := range testPlan {
		t.Run(p.name, func(t *testing.T) {
			if store.idx2str(p.idx) != p.res {
				t.Errorf("idx2str(%d) != '%s' (%s)", p.idx, p.res, store.idx2str(p.idx))
			}
			n, ok := store.str2idx(p.res)
			if !ok || n != p.idx {
				t.Errorf("str2idx(%s) != '%d' (%d)", p.res, p.idx, n)
			}
		})
	}
}

// Записываем длинные строки в хранилище и получаем строки-идентификаторы с закодированным на 1й позиции номером шарда
func TestStoreSaveLoad(t *testing.T) {
	cfg := config.GetConfig()
	drv := NewInMemoryDrv(cfg)
	store := NewStore(cfg, drv)

	testPlan := []plan{
		{name: "str1", arg: "qwe werqwe rqwe r", res: "AAA"},
		{name: "str2", arg: "qwe", res: "AAg"},
		{name: "str3", arg: "erqwer qwer qw", res: "ABA"},
		{name: "str4", arg: "ssdfsdf", res: "ABg"},
		{name: "str5", arg: "qwe", res: "AAg"},
	}

	for _, p := range testPlan {
		t.Run(p.name, func(t *testing.T) {
			id, err := store.Save(p.arg)
			if err != nil {
				t.Errorf("Save error %s", err)
			}
			if id != p.res {
				t.Errorf("Bad id: %s", id)
			}
			str, err := store.Load(id)
			if err != nil {
				t.Errorf("Cannot load id %s", id)
			}
			if str != p.arg {
				t.Errorf("Bad result for id=%s, res=%s", id, str)
			}
		})
	}

}
