package config

import (
	"errors"
	"maps"
)

var ErrCannotReadConfigFile = errors.New("cannot read config file")
var ErrBadJSONFormat = errors.New("bad json format")

// Config должен быть неизменяемым в коде, чтобы не получилась ситуация, когда тест для своих нужд изменяет
// конфиг, а потом "забывает" вернуть значение конфига в предыдущее состояние, а код в других местах
// теста при этом ломается.
// Поэтому изменяем конфиг путем копирования значений старого конфига в новый конфиг и модификации в новом конфиге нужных значений.
// Производительность:
//   Считаем, что структуры длиной до 128 байт передаются по значению без существенной просадки по производительности.
//   Модификация конфига - дорогая операция и нужна в основном в тестах

type Config struct {
	prms map[string]string
}

// Фабрика конфига
func GetConfig() Config {
	return Config{
		prms: map[string]string{
			"Version":      "0.0.1",
			"Listen":       GetCmdFlagListen(),
			"ShortBaseURL": GetCmdFlagShortBaseURL(),
			"RouterType":   "chi",
			"DBConfigPath": GetCmdFlagDBConfigPath(),
		},
	}
}

// Модификатор конфига
func (c Config) Modify(mVals map[string]string) Config {
	newConfig := Config{prms: make(map[string]string)}
	maps.Copy(newConfig.prms, c.prms)

	for pName := range newConfig.prms {
		newVal, ok := mVals[pName]
		if ok {
			newConfig.prms[pName] = newVal
		}
	}
	return newConfig
}

// Значения конфига
func (c Config) Version() string {
	return c.prms["Version"]
}

func (c Config) Listen() string {
	return c.prms["Listen"]
}

func (c Config) ShortBaseURL() string {
	return c.prms["ShortBaseURL"]
}

func (c Config) RouterType() string {
	return c.prms["RouterType"]
}

func (c Config) DBConfigPath() string {
	return c.prms["DBConfigPath"]
}
