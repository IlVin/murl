package main

import (
	"os"

	"murl/internal/config"
	"murl/internal/handlers"
	"murl/internal/repository"
	"murl/internal/service"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {

	// Логгер ZAP
	zapLogger, _ := zap.NewProduction()
	defer func() {
		if err := zapLogger.Sync(); err != nil {
			panic(err)
		}
	}()

	// Загрузка .env
	_ = godotenv.Load()

	// Конфигурация
	cmdArgs := os.Args[1:]
	cfg, err := config.NewConfig(&cmdArgs, nil, zapLogger)
	if err != nil {
		zapLogger.Sugar().Fatalw(err.Error(), "event", "start server")
	}
	cfg.Zap().Info("configuration initialized",
		zap.String("version", cfg.Version()),
		zap.String("router", cfg.RouterType()),
	)

	// Запускаем программу
	if err := run(cfg); err != nil {
		cfg.Zap().Fatal("server terminated with error", zap.Error(err))
	}
}

func run(cfg *config.Config) error {

	// Репозиторий
	repo := repository.NewRepo(cfg)

	// Сервис сокращателя: Работаем со строками, удовлетворяющими формату URL
	srv := service.NewService(cfg, repo)

	// HTTP хэндлеры, связанные вызовами с service
	h := handlers.NewHandlers(cfg, srv)

	// Ручки HTTP протокола
	router := handlers.WithLogging(cfg, handlers.NewRouter(cfg, h))

	// Запуск HTTP сервера
	cfg.Zap().Info("Starting server",
		zap.String("ListenAddr", cfg.ListenAddr()),
		zap.String("ShortBaseURL", cfg.ShortBaseURL().String()),
	)
	return handlers.Serve(cfg, router)
}
