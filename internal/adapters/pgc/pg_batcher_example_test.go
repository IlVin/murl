package pgc

import (
	"context"
	"fmt"
	"time"
)

type LogEntry struct {
	ID      int
	Message string
}

// ExamplePgBatcher_sync демонстрирует работу, когда основная горутина
// ожидает завершения обработки всех отправленных задач.
func ExamplePgBatcher_sync() {
	var db PgInstance
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Запрос, возвращающий данные (например, RETURNING id)
	query := NewQuery("INSERT INTO users (name) VALUES ($1) RETURNING id", func(u *User) []any {
		return []any{&u.ID}
	})

	// Инициализируем батчер
	batcher := NewPgBatcher[string](ctx, db, query)

	// Пишем в батчер в отдельной горутине
	go func() {
		defer batcher.Close() // Закрытие канала requests инициирует завершение работы

		names := []string{"Alice", "Bob", "Charlie"}
		for _, name := range names {
			batcher.Requests() <- BatchEntry[string]{
				Args: []any{name},
				Ctx:  name, // Используем имя как ключ для сопоставления результата
			}
		}
	}()

	// Вычитываем результаты в основной горутине.
	// Цикл завершится сам, когда батчер закроет канал Results после выполнения всех задач и ретраев.
	for res := range batcher.Results() {
		if res.Err != nil {
			fmt.Printf("Failed to insert %s: %v\n", res.Ctx, res.Err)
			continue
		}
		fmt.Printf("Successfully inserted %s with ID: %d\n", res.Ctx, res.Data.ID)
	}

	fmt.Println("All tasks processed.")
}

// ExamplePgBatcher_async демонстрирует максимально легкий режим.
// Поскольку используется NewCommand, воркер не будет слать успешные результаты в канал,
// что исключает блокировку при отсутствии читателя.
func ExamplePgBatcher_async() {
	var db PgInstance
	ctx := context.Background()

	// Режим Command: HasReturns() == false
	insertLog := NewCommand("INSERT INTO logs (msg) VALUES ($1)")
	batcher := NewPgBatcher[string](ctx, db, insertLog)

	go func() {
		defer batcher.Close()

		batcher.Requests() <- BatchEntry[string]{Args: []any{"System boot"}}
		batcher.Requests() <- BatchEntry[string]{Args: []any{"User login"}}
	}()

	// В режиме Command можно не вычитывать Results,
	// если вы не обрабатываете ошибки выполнения пакета.
}
