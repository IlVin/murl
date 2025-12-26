package repository

import (
	"murl/internal/config"
	"testing"
)

func TestUpSertSelect(t *testing.T) {
	// Конфигурация
	cfg := config.GetConfig()
	// Адаптер к определенной БД. Оперируем: вычислить шард, записать строку, прочитать до цифровому ID
	drv := NewInMemoryDrv(cfg)
	{
		shardID, idx, ok := drv.UpSert("1234")
		if !ok {
			t.Errorf("UpSert failed")
		}
		if idx != 0 {
			t.Errorf("Bad idx")
		}
		val, ok := drv.Select(shardID, idx)
		if !ok {
			t.Errorf("Select failed")
		}
		if val != "1234" {
			t.Errorf("Bad URL")
		}
	}
	{
		shardID, idx, ok := drv.UpSert("124")
		if !ok {
			t.Errorf("UpSert failed")
		}
		if idx != 1 {
			t.Errorf("Bad idx")
		}
		val, ok := drv.Select(shardID, idx)
		if !ok {
			t.Errorf("Select failed")
		}
		if val != "124" {
			t.Errorf("Bad URL")
		}
	}
	{
		shardID, idx, ok := drv.UpSert("1234")
		if !ok {
			t.Errorf("UpSert failed")
		}
		if idx != 0 {
			t.Errorf("Bad idx")
		}
		val, ok := drv.Select(shardID, idx)
		if !ok {
			t.Errorf("Select failed")
		}
		if val != "1234" {
			t.Errorf("Bad URL")
		}
	}
}
