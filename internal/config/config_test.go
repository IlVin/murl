package config

import (
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig(t *testing.T) {
	t.Run("Default values", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)
		assert.Equal(t, "0.0.1", cfg.Version())
		assert.Equal(t, "localhost:8080", cfg.ListenAddr())
		assert.Equal(t, "InMemory", cfg.RepoDrv())
	})

	t.Run("Flags priority", func(t *testing.T) {
		args := []string{
			"-a", "127.0.0.1:9090",
			"-b", "https://murl.ru",
			"-d", "postgres://user:pass@localhost:5432/db",
			"-f", "/tmp/events.log",
			"-audit-file", "/tmp/audit.log",
			"-audit-url", "https://audit.local",
		}

		// Создаем временные файлы, так как фабрика проверяет их существование (os.OpenFile)
		_ = os.WriteFile("/tmp/events.log", []byte(""), 0666)
		_ = os.WriteFile("/tmp/audit.log", []byte(""), 0666)
		defer os.Remove("/tmp/events.log")
		defer os.Remove("/tmp/audit.log")

		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:9090", cfg.ListenAddr())
		assert.Equal(t, "https://murl.ru", cfg.ShortBaseURL().String())
		assert.Equal(t, "PgDB", cfg.RepoDrv())
		assert.Equal(t, "/tmp/audit.log", cfg.AuditFile())
		assert.Equal(t, "https://audit.local", cfg.AuditURL().String())
	})

	t.Run("Env priority over flags", func(t *testing.T) {
		args := []string{"-a", "localhost:8080"}
		mockEnv := map[string]string{
			"SERVER_ADDRESS": "0.0.0.0:4444",
			"DATABASE_DSN":   "dsn_test",
			"BASE_URL":       "http://env.com",
		}

		lookup := func(key string) (string, bool) {
			val, ok := mockEnv[key]
			return val, ok
		}

		cfg, err := NewConfig(&args, lookup)
		require.NoError(t, err)
		assert.Equal(t, "0.0.0.0:4444", cfg.ListenAddr())
		assert.Equal(t, "http://env.com", cfg.ShortBaseURL().String())
		assert.Equal(t, "PgDB", cfg.RepoDrv())
	})

	t.Run("Invalid Flag Formats", func(t *testing.T) {
		tests := []struct {
			name string
			args []string
		}{
			{"bad address", []string{"-a", "wrong-format"}},
			{"bad url", []string{"-b", "://missing-scheme"}},
			{"bad audit url", []string{"-audit-url", "::%"}},
			{"bad event path", []string{"-f", "/non/existent/path/file"}},
			{"bad audit path", []string{"-audit-file", "/non/existent/path/audit"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := NewConfig(&tt.args, nil)
				assert.Error(t, err)
			})
		}
	})
}

func TestConfig_GettersSetters(t *testing.T) {
	cfg := Config{}

	t.Run("Immutability check", func(t *testing.T) {
		newCfg := cfg.SetVersion("2.0.0").
			SetJWTTTL(time.Hour).
			SetJWTSecretKey("secret").
			SetMaxBodySize(500).
			SetKeySession("new_session").
			SetShardSize(128).
			SetRouterType("gin").
			SetDBConfigPath("/etc/db.json").
			SetCompressibleContentTypes(map[string]struct{}{"text/plain": {}})

		assert.NotEqual(t, cfg.Version(), newCfg.Version())
		assert.Equal(t, "2.0.0", newCfg.Version())
		assert.Equal(t, time.Hour, newCfg.JWTTTL())
		assert.Equal(t, "secret", newCfg.JWTSecretKey())
		assert.Equal(t, int64(500), newCfg.MaxBodySize())
		assert.Equal(t, KeySession("new_session"), newCfg.KeySession())
		assert.Equal(t, byte(128), newCfg.ShardSize())
		assert.Equal(t, "gin", newCfg.RouterType())
		assert.Equal(t, "/etc/db.json", newCfg.DBConfigPath())
		assert.Contains(t, newCfg.CompressibleContentTypes(), "text/plain")
	})

	t.Run("SetShortBaseURL зануляет User", func(t *testing.T) {
		u, _ := url.Parse("https://host.com")
		sb := ShortBaseURL{*u}
		cfg = cfg.SetShortBaseURL(sb)

		assert.Nil(t, cfg.ShortBaseURL().User)
		assert.Equal(t, "https://host.com", cfg.ShortBaseURL().String())
	})
}

func TestSocketAddr(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		sa, err := NewSocketAddr("localhost:80")
		require.NoError(t, err)
		assert.Equal(t, "localhost:80", sa.String())
	})

	t.Run("Invalid", func(t *testing.T) {
		_, err := NewSocketAddr("no-port")
		assert.Error(t, err)
	})
}

func TestShortBaseURL(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		sb, err := NewShortBaseURL("http://murl.ru")
		require.NoError(t, err)
		assert.Equal(t, "http://murl.ru", sb.String())
	})

	t.Run("Invalid", func(t *testing.T) {
		_, err := NewShortBaseURL(" http://space-at-start")
		assert.Error(t, err)
	})
}

func TestEnvValidationErrors(t *testing.T) {
	// Специальные тесты для ошибочных ENV
	cases := []struct {
		envKey string
		envVal string
	}{
		{"SERVER_ADDRESS", "wrong"},
		{"BASE_URL", "::%"},
		{"FILE_STORAGE_PATH", "/un/exist/ent"},
		{"AUDIT_FILE", "/un/exist/ent"},
		{"AUDIT_URL", "::%"},
	}

	for _, c := range cases {
		t.Run(c.envKey, func(t *testing.T) {
			lookup := func(k string) (string, bool) {
				if k == c.envKey {
					return c.envVal, true
				}
				return "", false
			}
			_, err := NewConfig(nil, lookup)
			assert.Error(t, err)
		})
	}
}
