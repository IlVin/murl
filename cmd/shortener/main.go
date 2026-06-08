package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"murl/internal/adapters/audit"
	"murl/internal/config"
	"murl/internal/handlers"
	"murl/internal/handlers/middleware"
	"murl/internal/model/auditlog"
	"murl/internal/repository/repo"
	"murl/internal/service"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
)

var (
	buildVersion string = "N/A"
	buildDate    string = "N/A"
	buildCommit  string = "N/A"
)

//go:generate go run murl/cmd/reset ./../../internal/

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

	ctx := context.Background()

	// Загрузка .env
	_ = godotenv.Load()

	// Конфигурация
	cmdArgs := os.Args[1:]
	cfg, err := config.NewConfig(&cmdArgs, nil)
	if err != nil {
		return fmt.Errorf("failed to initialize config object: %w", err)
	}

	slog.Info("configuration initialized",
		slog.String("version", buildVersion),
		slog.String("date", buildDate),
		slog.String("commit", buildCommit),
	)

	fmt.Printf("Build version: %s\n", buildVersion)
	fmt.Printf("Build date: %s\n", buildDate)
	fmt.Printf("Build commit: %s\n", buildCommit)

	// Auditlog Observer
	auditlog := auditlog.NewAuditlog()
	auditlog.Start(3)     // Стартуем 3х воркеров для рассылки нотификаций
	defer auditlog.Stop() // Не забываем остановить воркеров

	slog.Info("created auditlog observer")

	// Добавляем потребителей, которые запишут нотификацию во всякие разные внешние места
	audit.AddAuditConsumers(cfg, auditlog)

	// Репозиторий
	repo, err := repo.NewRepo(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize the repository object: %w", err)
	}
	defer func() {
		err = repo.Close(ctx)
		if err != nil {
			slog.Error("repo close fail",
				slog.Any("err", err),
			)
		}
	}()

	// Сервис сокращателя: Работаем со строками, удовлетворяющими формату URL
	srv := service.NewService(ctx, cfg, repo, auditlog)

	// HTTP хэндлеры, связанные вызовами с service
	h := handlers.NewHandlers(cfg, srv)

	// Ручки HTTP протокола
	router, err := handlers.NewRouter(cfg, h)
	if err != nil {
		return err
	}

	// Интерцептор сессий
	sessionInterceptor, err := middleware.SessionInterceptor(cfg)
	if err != nil {
		return err
	}
	grpc := grpc.NewServer(
		grpc.UnaryInterceptor(sessionInterceptor),
	)

	// Запуск HTTP сервера
	slog.Info("Starting server",
		slog.String("ListenAddr", cfg.ListenAddr()),
		slog.String("ShortBaseURL", cfg.ShortBaseURL().String()),
	)
	return handlers.Serve(cfg, router, grpc)
}
