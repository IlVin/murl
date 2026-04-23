package audit

import (
	"fmt"
	"murl/internal/domain"
)

// ExampleURLAuditlog_Update демонстрирует процесс отправки уведомления.
func ExampleURLAuditlog_Update() {
	// Инициализируем аудитор с ID "main-logger" и целевым URL
	auditor := NewURLAuditlog("main-logger", "https://127.0.0.1")

	// Создаем тестовое уведомление
	notif := domain.Notification{
		Message: []byte(`{"event": "order_created", "order_id": 123}`),
	}

	// Отправляем уведомление.
	// Даже если контекст приложения будет отменен, запрос дойдет до конца
	// благодаря внутреннему таймауту HTTP-клиента.
	err := auditor.Update(notif)
	if err != nil {
		fmt.Printf("Notification failed: %v\n", err)
		return
	}

	fmt.Println("Notification sent successfully")
}

// ExampleURLAuditlog_GetID показывает получение идентификатора аудитора.
func ExampleURLAuditlog_GetID() {
	auditor := NewURLAuditlog("payment-auditor", "https://localhost:8080")

	fmt.Println(auditor.GetID())
	// Output: payment-auditor
}
