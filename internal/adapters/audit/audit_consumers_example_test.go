package audit

import (
	"murl/internal/model/auditlog"
	"net/url"
)

// MockConfig реализует интерфейс AuditlogConfig для примера.
type MockConfig struct {
	file string
	u    *url.URL
}

func (m *MockConfig) AuditFile() string  { return m.file }
func (m *MockConfig) AuditURL() *url.URL { return m.u }

// MockRegistry реализует интерфейс AuditlogSubscriber для примера.
type MockRegistry struct {
	consumers []auditlog.Subscriber
}

func (m *MockRegistry) Register(s auditlog.Subscriber) {
	m.consumers = append(m.consumers, s)
}
func (m *MockRegistry) UnRegister(s auditlog.Subscriber) {}

// ExampleAddAuditConsumers демонстрирует, как автоматически настроить адаптеры аудита
// на основе переданной конфигурации.
func ExampleAddAuditConsumers() {
	// 1. Подготавливаем конфигурацию
	u, _ := url.Parse("https://internal.net")
	cfg := &MockConfig{
		file: "/var/log/murl/audit.log",
		u:    u,
	}

	// 2. Инициализируем реестр (например, из доменного слоя)
	registry := &MockRegistry{}

	// 3. Регистрируем всех доступных потребителей одной командой.
	// Если в cfg.AuditFile() будет пустая строка, файловый аудитор создан не будет.
	AddAuditConsumers(cfg, registry)

	// Теперь registry содержит FileAuditlog и URLAuditlog
}
