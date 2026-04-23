package config

import (
	"fmt"
)

// ExampleNewConfig демонстрирует инициализацию конфигурации с использованием
// аргументов командной строки и переменных окружения.
func ExampleNewConfig() {
	// Имитируем аргументы командной строки
	args := []string{"-a", "localhost:9090", "-d", "postgres://user:pass@localhost/db"}

	// Имитируем переменные окружения
	mockEnv := func(key string) (string, bool) {
		if key == "BASE_URL" {
			return "https://murl.io", true
		}
		return "", false
	}

	cfg, err := NewConfig(&args, mockEnv)
	if err != nil {
		fmt.Printf("Error: %v", err)
		return
	}

	fmt.Printf("Listen: %s\n", cfg.ListenAddr())
	fmt.Printf("Repo: %s\n", cfg.RepoDrv())
	fmt.Printf("BaseURL: %s\n", cfg.ShortBaseURL())

	// Output:
	// Listen: localhost:9090
	// Repo: PgDB
	// BaseURL: https://murl.io
}

// ExampleConfig_SetShardSize показывает принцип иммутабельности сеттеров.
func ExampleConfig_SetShardSize() {
	cfg, _ := NewConfig(nil, nil)

	newCfg := cfg.SetShardSize(128)

	fmt.Printf("Old: %d, New: %d", cfg.ShardSize(), newCfg.ShardSize())
	// Output: Old: 64, New: 128
}
