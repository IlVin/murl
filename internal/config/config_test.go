package config

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetConfig(t *testing.T) {
	tests := []struct {
		name        string
		cmdArgs     []string
		env         map[string]string
		wantAddr    string
		wantBase    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "Успех: значения по умолчанию",
			cmdArgs:  []string{},
			env:      map[string]string{},
			wantAddr: "localhost:8080",
			wantBase: "http://localhost:8080/",
			wantErr:  false,
		},
		{
			name:     "Успех: флаги перекрывают дефолты",
			cmdArgs:  []string{"-a", "127.0.0.1:9090", "-b", "https://example.com"},
			env:      map[string]string{},
			wantAddr: "127.0.0.1:9090",
			wantBase: "https://example.com",
			wantErr:  false,
		},
		{
			name:    "Успех: ENV перекрывают флаги (наивысший приоритет)",
			cmdArgs: []string{"-a", "127.0.0.1:9090", "-b", "http://flags.io"},
			env: map[string]string{
				"SERVER_ADDRESS": "0.0.0.0:443",
				"BASE_URL":       "https://env.io",
			},
			wantAddr: "0.0.0.0:443",
			wantBase: "https://env.io",
			wantErr:  false,
		},
		{
			name:        "Ошибка: неверный формат адреса во флаге",
			cmdArgs:     []string{"-a", "invalid-address"},
			env:         map[string]string{},
			wantErr:     true,
			errContains: "invalid ListenAddr format",
		},
		{
			name:        "Ошибка: неверный формат URL во флаге",
			cmdArgs:     []string{"-b", "://wrong-url"},
			env:         map[string]string{},
			wantErr:     true,
			errContains: "invalid ShortBaseURL format",
		},
		{
			name:        "Ошибка: неверный адрес в ENV",
			cmdArgs:     []string{},
			env:         map[string]string{"SERVER_ADDRESS": "no-port"},
			wantErr:     true,
			errContains: "env SERVER_ADDRESS error",
		},
		{
			name:        "Ошибка: неверный URL в ENV",
			cmdArgs:     []string{},
			env:         map[string]string{"BASE_URL": "http:// invalid-space.com"},
			wantErr:     true,
			errContains: "env BASE_URL error",
		},
		{
			name:        "Ошибка: неизвестный флаг",
			cmdArgs:     []string{"-unknown", "val"},
			env:         map[string]string{},
			wantErr:     true,
			errContains: "failed to parse flags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLookup := func(key string) (string, bool) {
				val, ok := tt.env[key]
				return val, ok
			}

			cfg, err := GetConfig(&tt.cmdArgs, mockLookup)

			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorContains(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantAddr, cfg.ListenAddr())
				assert.Equal(t, tt.wantBase, cfg.ShortBaseURL())
			}
		})
	}
}

func TestConfig_ImmutabilityAndMethods(t *testing.T) {
	c := Config{version: "1.0", routerType: "chi"}

	// Проверка Version и SetVersion
	c2 := c.SetVersion("2.0")
	assert.Equal(t, "1.0", c.Version(), "Оригинал не должен измениться")
	assert.Equal(t, "2.0", c2.Version(), "Копия должна иметь новое значение")

	// Проверка RouterType и SetRouterType
	c3 := c.SetRouterType("fiber")
	assert.Equal(t, "chi", c.RouterType())
	assert.Equal(t, "fiber", c3.RouterType())

	// Проверка SetShortBaseURL и очистки User
	u, _ := url.Parse("http://host.com")
	sb := ShortBaseURL{URL: *u}
	c4 := c.SetShortBaseURL(sb)
	assert.Nil(t, c4.shortBaseURL.URL.User, "User должен быть занулен")
}

func TestSocketAddr_Coverage(t *testing.T) {
	// NewSocketAddr ошибка (прямой вызов для 100% покрытия)
	_, err := NewSocketAddr("localhost")
	assert.Error(t, err)

	// String()
	sa := SocketAddr{hostname: "127.0.0.1", port: "80"}
	assert.Equal(t, "127.0.0.1:80", sa.String())
}

func TestShortBaseURL_Coverage(t *testing.T) {
	// NewShortBaseURL ошибка
	_, err := NewShortBaseURL("://bad-scheme")
	assert.Error(t, err)

	// String()
	u, _ := url.Parse("https://google.com")
	sb := ShortBaseURL{URL: *u}
	assert.Equal(t, "https://google.com", sb.String())
}

func TestMust_Coverage(t *testing.T) {
	// Успешный кейс
	assert.NotPanics(t, func() {
		val := Must(NewSocketAddr("localhost:80"))
		assert.Equal(t, "localhost:80", val.String())
	})

	// Кейс с паникой
	assert.Panics(t, func() {
		Must(NewSocketAddr("invalid"))
	})
}

func TestGetConfig_NilLookup(t *testing.T) {
	// Покрытие ветки, где lookupEnv == nil (используется os.LookupEnv)
	_, err := GetConfig(nil, nil)
	assert.NoError(t, err)
}
