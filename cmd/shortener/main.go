package main

import (
	"flag"
	"log"

	"murl/internal/config"
	"murl/internal/handlers"
	"murl/internal/repository"
	"murl/internal/service"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {

	flag.Parse()

	// Конфигурация
	cfg := config.GetConfig()

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
	return handlers.Serve(cfg, router)
}
