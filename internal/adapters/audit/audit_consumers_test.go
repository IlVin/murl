package audit

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestAddAuditConsumers(t *testing.T) {
	tests := []struct {
		name     string
		fileRes  string
		urlRes   *url.URL
		expected int // сколько раз должен вызваться Register
	}{
		{
			name:     "Both file and URL provided",
			fileRes:  "/var/log/audit.log",
			urlRes:   &url.URL{Scheme: "http", Host: "localhost"},
			expected: 2,
		},
		{
			name:     "Only file provided",
			fileRes:  "/var/log/audit.log",
			urlRes:   nil,
			expected: 1,
		},
		{
			name:     "Only URL provided",
			fileRes:  "",
			urlRes:   &url.URL{Scheme: "http", Host: "localhost"},
			expected: 1,
		},
		{
			name:     "Nothing provided",
			fileRes:  "",
			urlRes:   nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockCfg := NewMockAuditlogConfig(ctrl)
			mockSub := NewMockAuditlogSubscriber(ctrl)

			// Настраиваем ожидания для конфига
			mockCfg.EXPECT().AuditFile().Return(tt.fileRes).AnyTimes()
			mockCfg.EXPECT().AuditURL().Return(tt.urlRes).AnyTimes()

			// Проверяем вызовы Register
			// Используем gomock.Any(), так как NewFileAuditlog создает новый объект внутри функции
			mockSub.EXPECT().Register(gomock.Any()).Times(tt.expected)

			AddAuditConsumers(mockCfg, mockSub)
		})
	}
}

// Тест для проверки корректности констант
func TestConstants(t *testing.T) {
	assert.Equal(t, "AuditFile", AuditFileID)
	assert.Equal(t, "AuditURL", AuditURLID)
}
