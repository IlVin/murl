package pgc

import (
	"context"
	"fmt"
)

// Предположим, у нас есть тип User
type User struct {
	ID   int
	Name string
}

// Пример использования Fetch для итерации по строкам
func ExampleFetch() {
	// Описываем SQL запрос
	sqlSelect := NewQuery(`
		SELECT id, name
		FROM users
		WHERE boss_id = $1
		LIMIT 10;
	`,
		func(u *User) []any {
			return []any{&u.ID, &u.Name}
		},
	).AsRead() // Это читающий запрос

	// Инициализируем вспомогательные структуры
	bossID := 34
	ctx := context.Background()
	pgInst, _ := NewPgConnector(ctx, "postgres://...")

	// В range цикле читаем результат
	for res, err := range Fetch(ctx, pgInst, sqlSelect, bossID) {
		if err != nil {
			panic(fmt.Errorf("failed to execute query (%s): %w", sqlSelect.Name(), err))
		}
		fmt.Printf("User: %v\n", res)
	}
}

// Пример использования FetchRow для получения одного объекта
func ExampleFetchRow() {
	// Описываем SQL запрос
	sqlSelect := NewQuery(`
		SELECT id, name
		FROM users
		WHERE boss_id = $1
		LIMIT 1;
	`,
		func(u *User) []any {
			return []any{&u.ID, &u.Name}
		},
	).AsRead() // Это читающий запрос

	// Инициализируем вспомогательные структуры
	bossID := 34
	ctx := context.Background()
	pgInst, _ := NewPgConnector(ctx, "postgres://...")

	// Запрос
	res, err := FetchRow(ctx, pgInst, sqlSelect, bossID)

	// Обрабатываем результат
	if err != nil {
		panic(fmt.Errorf("failed to execute query (%s): %w", sqlSelect.Name(), err))
	}
	fmt.Printf("User: %v\n", res)
}

// Пример выполнения команды Exec
func ExampleExec() {
	// Описываем SQL запрос
	sqlUpdate := NewCommand(`
		UPDATE users
		SET name=$1
		WHERE id=$2
	`).AsWrite() // Это записывающий запрос

	// Инициализируем вспомогательные структуры
	ctx := context.Background()
	pgInst, _ := NewPgConnector(ctx, "postgres://...")

	// Запрос
	res, err := Exec(ctx, pgInst, sqlUpdate, "New Name", 22)

	// Обрабатываем результат
	if err != nil {
		panic(fmt.Errorf("failed to execute query (%s): %w", sqlUpdate.Name(), err))
	}
	fmt.Printf("affected rows: %d", res)
}
