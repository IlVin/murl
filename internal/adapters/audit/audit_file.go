package audit

import (
	"fmt"
	"murl/internal/domain"
	"os"
	"syscall"
)

// FileAuditlog реализует запись уведомлений в локальный файл.
//
// Стратегия записи:
// Для обеспечения максимальной надежности и целостности данных при работе нескольких
// процессов с одним файлом, используется эксклюзивная блокировка на уровне ОС (flock).
// Каждая запись инициирует открытие файла, блокировку, запись, сброс буферов на диск (Sync)
// и закрытие. Этот подход гарантирует сохранность данных даже при внезапном сбое питания
// или конкурентной записи из разных инстансов приложения.
type FileAuditlog struct {
	auditFile string
	id        string
}

// NewFileAuditlog создает новый экземпляр файлового аудитора.
// Параметр auditFilePath указывает путь к файлу лога (будет создан, если не существует).
func NewFileAuditlog(id string, auditFilePath string) *FileAuditlog {
	return &FileAuditlog{
		auditFile: auditFilePath,
		id:        id,
	}
}

// GetID возвращает уникальный идентификатор аудитора.
func (f *FileAuditlog) GetID() string {
	return f.id
}

// writeBytes — вспомогательная функция для записи байтового среза с проверкой полноты записи.
func writeBytes(fh *os.File, msg []byte) error {
	n, err := fh.Write(msg)
	if err != nil {
		return fmt.Errorf("cannot write : %w", err)
	}
	if n != len(msg) {
		return fmt.Errorf("saved %d/%d bytes", n, len(msg))
	}
	return nil
}

// Update выполняет атомарную запись уведомления в файл.
// Процесс включает в себя:
// 1. Открытие файла в режиме добавления (O_APPEND).
// 2. Установку эксклюзивной блокировки syscall.LOCK_EX.
// 3. Запись тела сообщения и символа новой строки.
// 4. Принудительную синхронизацию с диском через fh.Sync.
func (f *FileAuditlog) Update(notif domain.Notification) error {
	fh, err := os.OpenFile(f.auditFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("cannot open file '%s': %w", f.auditFile, err)
	}
	defer fh.Close()

	// Устанавливаем эксклюзивную блокировку (flock).
	// syscall.LOCK_EX — блокируем на запись.
	if err := syscall.Flock(int(fh.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("audit log flock failed: %w", err)
	}
	defer syscall.Flock(int(fh.Fd()), syscall.LOCK_UN)

	if err := writeBytes(fh, notif.Message); err != nil {
		return fmt.Errorf("audit log write failed: %w", err)
	}

	if err := writeBytes(fh, []byte{'\n'}); err != nil {
		return fmt.Errorf("audit log newline failed: %w", err)
	}

	if err := fh.Sync(); err != nil {
		return fmt.Errorf("audit file sync failed: %w", err)
	}

	return nil
}
