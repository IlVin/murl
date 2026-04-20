package audit

import (
	"fmt"
	"murl/internal/domain"
	"os"
	"syscall"
)

/*
Стратегия записи в аудит файл: открыли, залочили на уровне OS, записали, закрыли.
Хоть и медленно, зато максимально надежно.
И для нескольких параллельно работающих на одной OS инстансов работает...
Если вдруг прихлынет высокая нагрузка, то можно открыть файл на постоянной основе,
сообразить мьютекс, чтобы воркеры выстраивались в очередь, fh.Sync вызывать не каждый раз,
а раз в секунду, например. Но это преждевременная оптимизация.
*/

type FileAuditlog struct {
	auditFile string
	id        string
}

// Конструктор
func NewFileAuditlog(id string, auditFilePath string) *FileAuditlog {
	return &FileAuditlog{
		auditFile: auditFilePath,
		id:        id,
	}
}

// GetID возвращает ID потребителя нотификаций
func (f *FileAuditlog) GetID() string {
	return f.id
}

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

// Update записывает нотификацию в файл
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
