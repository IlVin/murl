// Package audit предоставляет реализации потребителей для системы аудита (Audit Log).
//
// Пакет содержит адаптеры для записи уведомлений в локальные файлы с использованием
// блокировок на уровне ОС, а также для отправки данных во внешние системы по протоколу HTTP.
// Поддерживает автоматическую регистрацию потребителей на основе конфигурации.
package audit

import (
	"log/slog"
	"murl/internal/model/auditlog"
	"net/url"
)

//go:generate $GOPATH/bin/mockgen -source=$GOFILE -destination=audit_consumers_mock_test.go -package=$GOPACKAGE

const (
	// AuditFileID — уникальный идентификатор для файлового потребителя аудита.
	AuditFileID = "AuditFile"
	// AuditURLID — уникальный идентификатор для сетевого (HTTP) потребителя аудита.
	AuditURLID = "AuditURL"
)

// AuditlogConfig описывает интерфейс конфигурации, необходимой для инициализации потребителей.
type AuditlogConfig interface {
	// AuditFile возвращает путь к файлу лога. Если путь пуст, потребитель не создается.
	AuditFile() string
	// AuditURL возвращает URL для отправки нотификаций. Если nil, потребитель не создается.
	AuditURL() *url.URL
}

// AuditlogSubscriber определяет интерфейс реестра (Observer), в котором можно регистрировать потребителей.
type AuditlogSubscriber interface {
	// Register добавляет нового подписчика в систему уведомлений.
	Register(s auditlog.Subscriber)
	// UnRegister удаляет подписчика из системы.
	UnRegister(s auditlog.Subscriber)
}

// AddAuditConsumers выполняет автоматическую инициализацию и регистрацию потребителей аудита.
// Функция проверяет наличие настроек в конфигурации и, если они заданы, создает
// соответствующие адаптеры (File или URL) и регистрирует их в объекте AuditlogSubscriber.
func AddAuditConsumers(cfg AuditlogConfig, a AuditlogSubscriber) {
	// Инициализация файлового аудита
	if af := cfg.AuditFile(); af != "" {
		a.Register(NewFileAuditlog(AuditFileID, af))
		slog.Info("register file audit consumer",
			slog.String("audit_file", af),
		)
	}
	// Инициализация сетевого аудита
	if au := cfg.AuditURL(); au != nil {
		a.Register(NewURLAuditlog(AuditURLID, au.String()))
		slog.Info("register URL audit consumer",
			slog.String("audit_url", au.String()),
		)
	}
}
