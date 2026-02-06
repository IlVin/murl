package main

import (
	"log/slog"
	"os"

	"murl/internal/config"
	"murl/internal/handlers"
	"murl/internal/repository"
	"murl/internal/service"

	"github.com/joho/godotenv"
)

func main() {

	// Запускаем программу
	if err := run(); err != nil {
		slog.Error("server terminated with error",
			slog.Any("err", err),
		)
		os.Exit(1)
	}

}

func run() error {

	// Загрузка .env
	_ = godotenv.Load()

	// Конфигурация
	cmdArgs := os.Args[1:]
	cfg, err := config.NewConfig(&cmdArgs, nil)
	if err != nil {
		slog.Error("config error",
			slog.Any("err", err),
		)
		return err
	}
	slog.Info("configuration initialized",
		slog.String("version", cfg.Version()),
		slog.String("router", cfg.RouterType()),
	)

	// Репозиторий
	repo := repository.NewRepo(cfg)
	defer repo.Close()

	// Сервис сокращателя: Работаем со строками, удовлетворяющими формату URL
	srv := service.NewService(cfg, repo)

	// HTTP хэндлеры, связанные вызовами с service
	h := handlers.NewHandlers(cfg, srv)

	// Ручки HTTP протокола
	router := handlers.NewRouter(cfg, h)

	// Запуск HTTP сервера
	slog.Info("Starting server",
		slog.String("ListenAddr", cfg.ListenAddr()),
		slog.String("ShortBaseURL", cfg.ShortBaseURL().String()),
	)
	return handlers.Serve(cfg, router)
}
