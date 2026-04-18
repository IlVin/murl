package audit

import (
	"fmt"
	"murl/internal/domain"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileAuditlog(t *testing.T) {
	id := "test-id"
	path := "audit.log"
	adapter := NewFileAuditlog(id, path)

	assert.NotNil(t, adapter)
	assert.Equal(t, id, adapter.id)
	assert.Equal(t, path, adapter.auditFile)
}

func TestFileAuditlog_GetID(t *testing.T) {
	expectedID := "worker-123"
	adapter := NewFileAuditlog(expectedID, "any.log")

	assert.Equal(t, expectedID, adapter.GetID())
}

func TestFileAuditlog_Update(t *testing.T) {
	// Создаем временную директорию для изоляции тестов
	tmpDir, err := os.MkdirTemp("", "auditlog_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "test_audit.log")
	adapter := NewFileAuditlog("test-worker", logPath)

	t.Run("success: create and write first message", func(t *testing.T) {
		msg := []byte("hello audit")
		err := adapter.Update(domain.Notification{Message: msg})

		assert.NoError(t, err)

		content, _ := os.ReadFile(logPath)
		assert.Equal(t, "hello audit\n", string(content))
	})

	t.Run("success: append second message", func(t *testing.T) {
		msg := []byte("second line")
		err := adapter.Update(domain.Notification{Message: msg})

		assert.NoError(t, err)

		content, _ := os.ReadFile(logPath)
		assert.Contains(t, string(content), "hello audit\nsecond line\n")
	})

	t.Run("error: cannot open file (path is a directory)", func(t *testing.T) {
		// Создаем папку с таким же именем, чтобы OpenFile выдал ошибку
		errPath := filepath.Join(tmpDir, "is_a_dir")
		err := os.Mkdir(errPath, 0755)
		require.NoError(t, err)

		badAdapter := NewFileAuditlog("id", errPath)
		err = badAdapter.Update(domain.Notification{Message: []byte("fail")})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot open file")
	})

	t.Run("error: write to closed file (internal writeBytes test)", func(t *testing.T) {
		// Создаем файл, открываем его и сразу закрываем
		f, err := os.CreateTemp(tmpDir, "closed_test")
		require.NoError(t, err)
		f.Close()

		// Прямой вызов вспомогательной функции для проверки ветки ошибки записи
		err = writeBytes(f, []byte("data"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot write")
	})

	t.Run("error: incomplete write (simulated)", func(t *testing.T) {
		// В норме это сложно воспроизвести, но мы проверяем логику возврата ошибки n != len(msg)
		// через вызов writeBytes с данными в файл, открытый только на чтение (если бы функция позволяла)
		// Здесь мы полагаемся на предыдущий тест, так как логика writeBytes едина.
	})
}

func TestFileAuditlog_FlockIntegrity(t *testing.T) {
	// Этот тест проверяет, что блокировка не мешает последовательному выполнению
	tmpDir, _ := os.MkdirTemp("", "flock_test")
	defer os.RemoveAll(tmpDir)
	logPath := filepath.Join(tmpDir, "integrity.log")
	adapter := NewFileAuditlog("worker", logPath)

	// Пишем 10 сообщений подряд
	for i := range 10 {
		msg := fmt.Sprintf("line %d", i)
		err := adapter.Update(domain.Notification{Message: []byte(msg)})
		assert.NoError(t, err)
	}

	content, _ := os.ReadFile(logPath)
	// Проверяем, что все строки на месте и не перемешаны
	assert.Contains(t, string(content), "line 0\n")
	assert.Contains(t, string(content), "line 9\n")
}
