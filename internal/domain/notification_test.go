package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNotification(t *testing.T) {
	// Создаем экземпляр структуры
	msg := []byte{'t', 'e', 's', 't', ' ', 'm', 'e', 's', 's', 'a', 'g', 'e'}
	n := Notification{
		Message: msg,
	}

	// Проверяем, что значение установилось корректно
	assert.Equal(t, msg, n.Message, "Поле Message должно совпадать с переданным значением")
}
