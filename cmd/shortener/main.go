package main

import (
	"fmt"
	"log"
	"os"

	"murl/internal/config"
	"murl/internal/handlers"
	"murl/internal/repository"
	"murl/internal/service"

	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Загрузка .env
	_ = godotenv.Load()

	// Конфигурация
	cmdArgs := os.Args[1:]
	cfg, err := config.GetConfig(&cmdArgs, nil)
	fmt.Fprintf(os.Stderr, "Shortener server v%s\n", cfg.Version())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot load config:\n%v\n", err)
		os.Exit(500)
	}

	// Адаптер к определенной БД. Оперируем: вычислить шард, записать строку, прочитать до цифровому ID
	drv := repository.NewInMemoryDrv(cfg)

	// Схема хранилища: Записать строку в БД и получить строковый идентификатор этой записи
	store := repository.NewStore(cfg, drv)

	// Сервис сокращателя: Работаем со строками, удовлетворяющими формату URL
	service := service.NewService(cfg, store)

	// HTTP хэндлеры, связанные вызовами с service
	hndlrs := handlers.NewHandlers(cfg, service)

	// Ручки HTTP протокола
	router := handlers.NewRouter(cfg, hndlrs)

	// Запуск HTTP сервера
	fmt.Fprintf(os.Stderr, "Listen on [%s]\nShort base URL is [%s]\n", cfg.ListenAddr(), cfg.ShortBaseURL())
	return handlers.Serve(cfg, router)
}
