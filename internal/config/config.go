package config

import (
	"maps"
)

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
			"Listen":       cmdFlags.Listen.String(),
			"ShortBaseURL": cmdFlags.ShortBaseURL.String(),
			"RouterType":   "chi",
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
func (c Config) Listen() string {
	return c.prms["Listen"]
}

func (c Config) ShortBaseURL() string {
	return c.prms["ShortBaseURL"]
}

func (c Config) RouterType() string {
	return c.prms["RouterType"]
}
