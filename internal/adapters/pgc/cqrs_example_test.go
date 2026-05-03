package pgc

import (
	"context"
	"fmt"
	"log"
	"log/slog"
)

// ExampleNewCQRSConnector демонстрирует создание CQRS коннектора и его использование
// для автоматического разделения Read/Write запросов.
func ExampleNewCQRSConnector() {
	// В реальном коде здесь будет инициализация pgxpool через NewConnector
	var master PgInstance
	var replica1 PgInstance
	var replica2 PgInstance

	// Инициализируем CQRS коннектор
	db := NewCQRSConnector(master, replica1, replica2)

	ctx := context.Background()

	// 1. Единичный Read-Only запрос (уйдет на одну из реплик)
	findUser := NewQuery("SELECT name FROM users WHERE id = $1", func(u *User) []any {
		return []any{&u.Name}
	}).AsRead()

	user, err := FetchRow(ctx, db, findUser, 42)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(user.Name)

	// 2. Единичный Write запрос (всегда уйдет на мастер)
	updateName := NewCommand("UPDATE users SET name = $1 WHERE id = $2").AsWrite()

	affected, err := Exec(ctx, db, updateName, "NewName", 42)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Rows affected: %d\n", affected)
}

// ExampleNewCQRSConnector_batcher показывает, как CQRS коннектор работает в связке с батчером.
// Батчер будет отправлять пакеты на реплики или мастер в зависимости от настроек Query.
func ExampleNewCQRSConnector_batcher() {
	var master PgInstance
	var replica PgInstance

	// Создаем CQRS коннектор
	db := NewCQRSConnector(master, replica)
	ctx := context.Background()

	// Описываем команду на вставку логов (Write-only)
	insertLog := NewCommand("INSERT INTO logs (msg) VALUES ($1)").AsWrite()

	// Создаем батчер, передавая ему наш CQRS коннектор.
	// Батчер будет вызывать SendBatch у коннектора, а тот направит пакет на мастер.
	batcher := NewPgBatcher[int](ctx, db, insertLog)

	go func() {
		defer func() {
			if err := batcher.Close(); err != nil {
				slog.Error("close batcher fail",
					slog.Any("err", err),
				)
			}
		}()
		batcher.Requests() <- BatchEntry[int]{Args: []any{"System event"}, Ctx: 1}
	}()

	// Вычитываем результаты (ошибки выполнения пакета на мастере)
	for res := range batcher.Results() {
		if res.Err != nil {
			fmt.Printf("Batch error: %v\n", res.Err)
		}
	}
}
