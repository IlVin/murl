package audit_test

import (
	"fmt"
	"murl/internal/adapters/audit"
	"murl/internal/domain"
	"os"
)

// ExampleFileAuditlog_Update демонстрирует надежную запись уведомления в файл лога.
func ExampleFileAuditlog_Update() {
	tempFile := "example_audit.log"
	defer os.Remove(tempFile) // Очистка после примера

	// Создаем аудитор
	auditor := audit.NewFileAuditlog("local-file-logger", tempFile)

	// Подготавливаем данные
	notif := domain.Notification{
		Message: []byte(`{"level":"info","msg":"audit record"}`),
	}

	// Записываем данные. Операция заблокирует файл для других процессов на время записи.
	err := auditor.Update(notif)
	if err != nil {
		fmt.Printf("File audit failed: %v\n", err)
		return
	}

	// Проверим результат
	content, _ := os.ReadFile(tempFile)
	fmt.Printf("Logged: %s", string(content))

	// Output:
	// Logged: {"level":"info","msg":"audit record"}
}

// ExampleFileAuditlog_GetID демонстрирует получение идентификатора файлового аудитора.
func ExampleFileAuditlog_GetID() {
	auditor := audit.NewFileAuditlog("file-01", "/tmp/audit.log")

	fmt.Println(auditor.GetID())
	// Output: file-01
}
