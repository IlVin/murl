package config

import (
	"fmt"
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
			"--audit-file", "/tmp/audit.log",
			"--audit-url", "https://audit.local",
		}

		// Создаем временные файлы
		_ = os.WriteFile("/tmp/events.log", []byte(""), 0666)
		_ = os.WriteFile("/tmp/audit.log", []byte(""), 0666)
		defer func() {
			err := os.Remove("/tmp/events.log")
			assert.NoError(t, err)
		}()
		defer func() {
			err := os.Remove("/tmp/audit.log")
			assert.NoError(t, err)
		}()

		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:9090", cfg.ListenAddr())
		assert.Equal(t, "https://murl.ru", cfg.ShortBaseURL().String())
		assert.Equal(t, "PgDB", cfg.RepoDrv())
		assert.Equal(t, "/tmp/audit.log", cfg.AuditFile())
		// assert.Equal(t, "https://audit.local", cfg.AuditURL().String())
		// assert.Equal(t, "/tmp/events.log", cfg.EventStoragePath())
	})

	t.Run("Empty paths in flags and env", func(t *testing.T) {
		// Пустые строки в путях не должны вызывать ошибок (ветки if s == "" { return nil })
		args := []string{"-f", "", "--audit-file", "", "--audit-url", ""}
		mockEnv := map[string]string{
			"FILE_STORAGE_PATH": "",
			"AUDIT_FILE":        "",
			"AUDIT_URL":         "",
		}
		lookup := func(key string) (string, bool) {
			val, ok := mockEnv[key]
			return val, ok
		}

		cfg, err := NewConfig(&args, lookup)
		require.NoError(t, err)
		assert.Empty(t, cfg.EventStoragePath())
		assert.Empty(t, cfg.AuditFile())
		assert.Nil(t, cfg.AuditURL())
	})

	t.Run("Empty paths in flags and env", func(t *testing.T) {
		// Пустые строки в путях не должны вызывать ошибок (ветки if s == "" { return nil })
		args := []string{"--cert-file", "", "--key-file", "", "-s", ""}
		mockEnv := map[string]string{
			"CERT_FILE":    "",
			"KEY_FILE":     "",
			"CONFIG":       "",
			"ENABLE_HTTPS": "",
		}
		lookup := func(key string) (string, bool) {
			val, ok := mockEnv[key]
			return val, ok
		}

		cfg, err := NewConfig(&args, lookup)
		require.NoError(t, err)
		assert.Empty(t, cfg.EventStoragePath())
		assert.Empty(t, cfg.AuditFile())
		assert.Empty(t, cfg.KeyFile())
		assert.Empty(t, cfg.CertFile())
		assert.Empty(t, cfg.ConfigFile())
		assert.Nil(t, cfg.AuditURL())
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
			{"bad audit url", []string{"--audit-url", "::%"}},
			{"bad event path", []string{"-f", "/non/existent/path/file"}},
			{"bad audit path", []string{"--audit-file", "/non/existent/path/audit"}},
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

	t.Run("Full chain set and get", func(t *testing.T) {
		u, _ := url.Parse("https://audit.url")
		contentTypes := map[string]struct{}{"application/xml": {}}

		newCfg := cfg.
			SetVersion("2.0.0").
			SetJWTTTL(time.Hour).
			SetJWTSecretKey("secret").
			SetMaxBodySize(500).
			SetKeySession("new_session").
			SetShardSize(128).
			SetRouterType("gin").
			SetDBConfigPath("/etc/db.json").
			SetDBDSN("postgres://...").
			SetRepoDrv("PgDB").
			SetEventStoragePath("/tmp/ev").
			SetAuditFile("/tmp/au").
			SetAuditURL(u).
			SetCompressibleContentTypes(contentTypes).
			SetEnabledHTTPS(true).
			SetCertFile("/tmp/cert").
			SetKeyFile("/tmp/key").
			SetConfigFile("/tmp/config")

		assert.Equal(t, "2.0.0", newCfg.Version())
		assert.Equal(t, time.Hour, newCfg.JWTTTL())
		assert.Equal(t, "secret", newCfg.JWTSecretKey())
		assert.Equal(t, int64(500), newCfg.MaxBodySize())
		assert.Equal(t, KeySession("new_session"), newCfg.KeySession())
		assert.Equal(t, byte(128), newCfg.ShardSize())
		assert.Equal(t, "gin", newCfg.RouterType())
		assert.Equal(t, "/etc/db.json", newCfg.DBConfigPath())
		assert.Equal(t, "postgres://...", newCfg.DBDSN())
		assert.Equal(t, "PgDB", newCfg.RepoDrv())
		assert.Equal(t, "/tmp/ev", newCfg.EventStoragePath())
		assert.Equal(t, "/tmp/au", newCfg.AuditFile())
		assert.Equal(t, u, newCfg.AuditURL())
		assert.Equal(t, contentTypes, newCfg.CompressibleContentTypes())
		assert.Equal(t, true, newCfg.EnabledHTTPS())
		assert.Equal(t, "/tmp/cert", newCfg.CertFile())
		assert.Equal(t, "/tmp/key", newCfg.KeyFile())
		assert.Equal(t, "/tmp/config", newCfg.ConfigFile())
	})

	t.Run("SetShortBaseURL user stripping", func(t *testing.T) {
		u, _ := url.Parse("https://host.com")
		sb := ShortBaseURL{*u}
		cfg = cfg.SetShortBaseURL(sb)

		assert.Nil(t, cfg.ShortBaseURL().User)
		assert.Equal(t, "https://host.com", cfg.ShortBaseURL().String())
	})

	t.Run("SetListenAddr", func(t *testing.T) {
		sa, _ := NewSocketAddr("localhost:9999")
		cfg = cfg.SetListenAddr(sa)
		assert.Equal(t, "localhost:9999", cfg.ListenAddr())
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
	cases := []struct {
		envKey string
		envVal string
	}{
		{"SERVER_ADDRESS", "wrong"},
		{"BASE_URL", "::%"},
		{"FILE_STORAGE_PATH", "/un/exist/ent/path/file"},
		{"AUDIT_FILE", "/un/exist/ent/path/audit"},
		{"CERT_FILE", "/un/exist/ent/path/cert"},
		{"KEY_FILE", "/un/exist/ent/path/key"},
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

func TestJSONConfig(t *testing.T) {
	certPath := "/tmp/test.crt"
	keyPath := "/tmp/test.key"
	auditPath := "/tmp/json-audit.log"
	configPath := "/tmp/config_test.json"

	// Создаем окружение
	_ = os.WriteFile(certPath, []byte("cert"), 0666)
	_ = os.WriteFile(keyPath, []byte("key"), 0666)
	_ = os.WriteFile(auditPath, []byte(""), 0666)

	defer func() {
		os.Remove(certPath)
		os.Remove(keyPath)
		os.Remove(auditPath)
		os.Remove(configPath)
	}()

	fullJSON := fmt.Sprintf(`{
			"server_address": "127.0.0.1:7070",
			"base_url": "https://json-url.com",
			"database_dsn": "postgres://json",
			"enable_https": true,
			"cert_file": "%s",
			"key_file": "%s",
			"audit_file": "%s"
		}`, certPath, keyPath, auditPath)
	_ = os.WriteFile(configPath, []byte(fullJSON), 0666)

	t.Run("Read full config with HTTPS", func(t *testing.T) {
		args := []string{"-c", configPath}
		cfg, err := NewConfig(&args, nil)

		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:7070", cfg.ListenAddr())
		assert.Equal(t, "https://json-url.com", cfg.ShortBaseURL().String())
		assert.Equal(t, "postgres://json", cfg.DBDSN())
		assert.True(t, cfg.EnabledHTTPS())
	})

	t.Run("HTTPS remains disabled without cert/key", func(t *testing.T) {
		incompleteJSON := `{"enable_https": true, "server_address": "127.0.0.1:8080"}`
		_ = os.WriteFile(configPath, []byte(incompleteJSON), 0666)

		args := []string{"-c", configPath}
		cfg, err := NewConfig(&args, nil)

		require.NoError(t, err)
		assert.False(t, cfg.EnabledHTTPS(), "Should be false because cert/key missing in JSON")
	})

	t.Run("Read from JSON file via ENV", func(t *testing.T) {
		// Используем файл, оставшийся от предыдущего теста (адрес 8080)
		mockEnv := map[string]string{"CONFIG": configPath}
		lookup := func(k string) (string, bool) { return mockEnv[k], true }

		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)
		assert.Equal(t, configPath, cfg.ConfigFile())
		assert.Equal(t, "127.0.0.1:8080", cfg.ListenAddr())
	})

	t.Run("JSON parse error", func(t *testing.T) {
		badJSONPath := "/tmp/bad_json.json"
		_ = os.WriteFile(badJSONPath, []byte("{ invalid json"), 0666)
		defer os.Remove(badJSONPath)

		args := []string{"-c", badJSONPath}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
	})

	t.Run("JSON non-existent file", func(t *testing.T) {
		args := []string{"-c", "/tmp/missing_file_999.json"}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
	})
}

func TestConfig_PriorityChain(t *testing.T) {
	// 1. JSON (самый низкий после default)
	configPath := "/tmp/priority.json"
	_ = os.WriteFile(configPath, []byte(`{"server_address": "json:1"}`), 0666)
	defer os.Remove(configPath)

	// 2. Флаг (перекрывает JSON)
	args := []string{"-c", configPath, "-a", "flag:2"}

	// 3. ENV (перекрывает Флаг)
	mockEnv := map[string]string{"SERVER_ADDRESS": "env:3"}
	lookup := func(k string) (string, bool) {
		val, ok := mockEnv[k]
		return val, ok
	}

	cfg, err := NewConfig(&args, lookup)
	require.NoError(t, err)

	// В итоге должен победить ENV
	assert.Equal(t, "env:3", cfg.ListenAddr())
}

func TestConfig_SpecialCases(t *testing.T) {
	t.Run("Invalid path in JSON fields", func(t *testing.T) {
		path := "/tmp/invalid_fields.json"
		// Путь к файлу, который нельзя создать
		_ = os.WriteFile(path, []byte(`{"file_storage_path": "/proc/invalid/path"}`), 0666)
		defer os.Remove(path)

		args := []string{"-c", path}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
	})

	t.Run("Invalid URL in JSON", func(t *testing.T) {
		path := "/tmp/invalid_url.json"
		_ = os.WriteFile(path, []byte(`{"audit_url": "::%"}`), 0666)
		defer os.Remove(path)

		args := []string{"-c", path}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
	})
}
