package config

import (
	"fmt"
	"net/netip"
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
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
		_ = os.Remove(auditPath)
		_ = os.Remove(configPath)
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
		defer func() {
			_ = os.Remove(badJSONPath)
		}()

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
	defer func() {
		_ = os.Remove(configPath)
	}()

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
		defer func() {
			_ = os.Remove(path)
		}()

		args := []string{"-c", path}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
	})

	t.Run("Invalid URL in JSON", func(t *testing.T) {
		path := "/tmp/invalid_url.json"
		_ = os.WriteFile(path, []byte(`{"audit_url": "::%"}`), 0666)
		defer func() {
			_ = os.Remove(path)
		}()

		args := []string{"-c", path}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
	})
}

// TestConfig_HTTPSSettings тестирует все HTTPS настройки
func TestConfig_HTTPSFullCoverage(t *testing.T) {
	t.Run("Enable HTTPS via flag -s with true values", func(t *testing.T) {
		testCases := []struct {
			flagValue string
			expected  bool
		}{
			{"1", true},
			{"true", true},
			{"TRUE", true}, // должен сработать else
			{"any", true},
			{"", false}, // пустая строка должна выключить
		}

		for _, tc := range testCases {
			t.Run(tc.flagValue, func(t *testing.T) {
				args := []string{"-s", tc.flagValue}
				cfg, err := NewConfig(&args, nil)
				require.NoError(t, err)
				assert.Equal(t, tc.expected, cfg.EnabledHTTPS())
			})
		}
	})

	t.Run("Enable HTTPS via ENV", func(t *testing.T) {
		testCases := []struct {
			envValue string
			expected bool
		}{
			{"1", true},
			{"true", true},
			{"0", false},
			{"false", false},
			{"", false},
			{"anything", true}, // любое непустое значение кроме 0/false = true
		}

		for _, tc := range testCases {
			t.Run(tc.envValue, func(t *testing.T) {
				mockEnv := map[string]string{"ENABLE_HTTPS": tc.envValue}
				lookup := func(k string) (string, bool) {
					val, ok := mockEnv[k]
					return val, ok
				}
				cfg, err := NewConfig(nil, lookup)
				require.NoError(t, err)
				assert.Equal(t, tc.expected, cfg.EnabledHTTPS())
			})
		}
	})

	t.Run("HTTPS with cert and key files via JSON", func(t *testing.T) {
		certPath := "/tmp/https_test.crt"
		keyPath := "/tmp/https_test.key"
		configPath := "/tmp/https_config.json"

		err := os.WriteFile(certPath, []byte("cert"), 0666)
		require.NoError(t, err)
		err = os.WriteFile(keyPath, []byte("key"), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(certPath)
		}()
		defer func() {
			_ = os.Remove(keyPath)
		}()

		jsonContent := `{
			"enable_https": true,
			"cert_file": "` + certPath + `",
			"key_file": "` + keyPath + `"
		}`
		err = os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		args := []string{"-c", configPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)

		assert.True(t, cfg.EnabledHTTPS())
		assert.Equal(t, certPath, cfg.CertFile())
		assert.Equal(t, keyPath, cfg.KeyFile())
	})

	t.Run("HTTPS cert/key via ENV", func(t *testing.T) {
		certPath := "/tmp/env_test.crt"
		keyPath := "/tmp/env_test.key"

		err := os.WriteFile(certPath, []byte("cert"), 0666)
		require.NoError(t, err)
		err = os.WriteFile(keyPath, []byte("key"), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(certPath)
		}()
		defer func() {
			_ = os.Remove(keyPath)
		}()

		mockEnv := map[string]string{
			"ENABLE_HTTPS": "true",
			"CERT_FILE":    certPath,
			"KEY_FILE":     keyPath,
		}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}

		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)

		assert.True(t, cfg.EnabledHTTPS())
		assert.Equal(t, certPath, cfg.CertFile())
		assert.Equal(t, keyPath, cfg.KeyFile())
	})

	t.Run("HTTPS cert/key via flags", func(t *testing.T) {
		certPath := "/tmp/flag_test.crt"
		keyPath := "/tmp/flag_test.key"

		err := os.WriteFile(certPath, []byte("cert"), 0666)
		require.NoError(t, err)
		err = os.WriteFile(keyPath, []byte("key"), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(certPath)
		}()
		defer func() {
			_ = os.Remove(keyPath)
		}()

		args := []string{
			"-s", "true",
			"--cert-file", certPath,
			"--key-file", keyPath,
		}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)

		assert.True(t, cfg.EnabledHTTPS())
		assert.Equal(t, certPath, cfg.CertFile())
		assert.Equal(t, keyPath, cfg.KeyFile())
	})
}

// TestConfig_ConfigFile тестирование ConfigFile геттера/сеттера
func TestConfig_ConfigFileMethods(t *testing.T) {
	t.Run("Set and get ConfigFile", func(t *testing.T) {
		cfg := Config{}
		newCfg := cfg.SetConfigFile("/path/to/config.json")
		assert.Equal(t, "/path/to/config.json", newCfg.ConfigFile())
	})

	t.Run("ConfigFile remains empty by default", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)
		assert.Empty(t, cfg.ConfigFile())
	})

	t.Run("ConfigFile from ENV", func(t *testing.T) {
		configPath := "/tmp/env_config.json"
		jsonContent := `{"server_address": "env:9090"}`
		err := os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		mockEnv := map[string]string{"CONFIG": configPath}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}

		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)
		assert.Equal(t, configPath, cfg.ConfigFile())
		assert.Equal(t, "env:9090", cfg.ListenAddr())
	})

	t.Run("ConfigFile from flag -c", func(t *testing.T) {
		configPath := "/tmp/flag_config.json"
		jsonContent := `{"server_address": "flag:8080"}`
		err := os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		args := []string{"-c", configPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Equal(t, configPath, cfg.ConfigFile())
		assert.Equal(t, "flag:8080", cfg.ListenAddr())
	})

	t.Run("ConfigFile from --config flag", func(t *testing.T) {
		configPath := "/tmp/long_flag_config.json"
		jsonContent := `{"server_address": "long:7777"}`
		err := os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		args := []string{"--config", configPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Equal(t, configPath, cfg.ConfigFile())
		assert.Equal(t, "long:7777", cfg.ListenAddr())
	})

	t.Run("ConfigFile from flag with non-existent file", func(t *testing.T) {
		args := []string{"-c", "/tmp/does_not_exist_12345.json"}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot open file")
	})

	t.Run("ConfigFile from ENV with empty value", func(t *testing.T) {
		mockEnv := map[string]string{"CONFIG": ""}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}
		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)
		assert.Empty(t, cfg.ConfigFile())
	})
}

// TestConfig_ShardSize тестирование размера шардов
func TestConfig_ShardSize(t *testing.T) {
	t.Run("Default shard size", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)
		assert.Equal(t, byte(64), cfg.ShardSize())
	})

	t.Run("Set custom shard size", func(t *testing.T) {
		cfg := Config{}
		newCfg := cfg.SetShardSize(128)
		assert.Equal(t, byte(128), newCfg.ShardSize())
	})

	t.Run("Shard size zero", func(t *testing.T) {
		cfg := Config{}
		newCfg := cfg.SetShardSize(0)
		assert.Equal(t, byte(0), newCfg.ShardSize())
	})
}

// TestConfig_AuditURLFull тестирование всех сценариев AuditURL
func TestConfig_AuditURLFull(t *testing.T) {
	t.Run("Set AuditURL via JSON with URL", func(t *testing.T) {
		configPath := "/tmp/audit_url_test.json"
		jsonContent := `{"audit_url": "https://audit.example.com/api"}`
		err := os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		args := []string{"-c", configPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)

		assert.NotNil(t, cfg.AuditURL())
		assert.Equal(t, "https://audit.example.com/api", cfg.AuditURL().String())
	})

	t.Run("AuditURL remains nil when not set", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)
		assert.Nil(t, cfg.AuditURL())
	})

	t.Run("Set AuditURL via flag --audit-url", func(t *testing.T) {
		args := []string{"--audit-url", "http://localhost:9999/audit"}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)

		assert.NotNil(t, cfg.AuditURL())
		assert.Equal(t, "http://localhost:9999/audit", cfg.AuditURL().String())
	})

	t.Run("Set AuditURL via ENV", func(t *testing.T) {
		mockEnv := map[string]string{"AUDIT_URL": "https://env-audit.com"}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}
		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)

		assert.NotNil(t, cfg.AuditURL())
		assert.Equal(t, "https://env-audit.com", cfg.AuditURL().String())
	})

	t.Run("Empty AuditURL in flag", func(t *testing.T) {
		args := []string{"--audit-url", ""}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Nil(t, cfg.AuditURL())
	})
}

// TestConfig_DefaultValuesAfterSet тестирует иммутабельность
func TestConfig_DefaultValuesAfterSet(t *testing.T) {
	t.Run("Original config unchanged after Set operations", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)

		originalVersion := cfg.Version()
		originalAddr := cfg.ListenAddr()

		newCfg := cfg.SetVersion("2.0.0")
		newCfg = newCfg.SetListenAddr(SocketAddr{hostname: "new", port: "9090"})

		assert.Equal(t, originalVersion, cfg.Version())
		assert.Equal(t, originalAddr, cfg.ListenAddr())

		assert.Equal(t, "2.0.0", newCfg.Version())
		assert.Equal(t, "new:9090", newCfg.ListenAddr())
	})
}

// TestConfig_MaxBodySize тестирование максимального размера тела
func TestConfig_MaxBodySize(t *testing.T) {
	t.Run("Default max body size", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(1024*1024), cfg.MaxBodySize())
	})

	t.Run("Set custom max body size", func(t *testing.T) {
		cfg := Config{}
		newCfg := cfg.SetMaxBodySize(2048)
		assert.Equal(t, int64(2048), newCfg.MaxBodySize())
	})
}

// TestConfig_JWT тестирование JWT настроек
func TestConfig_JWT(t *testing.T) {
	t.Run("Default JWT settings", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)

		assert.Equal(t, "jwtSecretKey+jwtSecretKey-jwtSecretKey+jwtSecretKey-jwtSecretKey", cfg.JWTSecretKey())
		assert.Equal(t, 30*24*time.Hour, cfg.JWTTTL())
		assert.Equal(t, KeySession("session"), cfg.KeySession())
	})

	t.Run("Custom JWT settings", func(t *testing.T) {
		cfg := Config{}
		newCfg := cfg.
			SetJWTSecretKey("my-secret-key").
			SetJWTTTL(24 * time.Hour).
			SetKeySession("custom_session")

		assert.Equal(t, "my-secret-key", newCfg.JWTSecretKey())
		assert.Equal(t, 24*time.Hour, newCfg.JWTTTL())
		assert.Equal(t, KeySession("custom_session"), newCfg.KeySession())
	})
}

// TestConfig_CompressibleContentTypes тестирование типов для сжатия
func TestConfig_CompressibleContentTypes(t *testing.T) {
	t.Run("Default content types", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)

		types := cfg.CompressibleContentTypes()
		assert.Contains(t, types, "application/json")
		assert.Contains(t, types, "text/html")
		assert.Contains(t, types, "text/css")
		assert.Contains(t, types, "application/javascript")
		assert.Len(t, types, 4)
	})

	t.Run("Set custom content types", func(t *testing.T) {
		cfg := Config{}
		customTypes := map[string]struct{}{
			"application/xml": {},
			"text/plain":      {},
		}
		newCfg := cfg.SetCompressibleContentTypes(customTypes)
		assert.Equal(t, customTypes, newCfg.CompressibleContentTypes())
	})
}

// TestConfig_JSONFields тестирование всех полей JSON конфига
func TestConfig_JSONAllFields(t *testing.T) {
	t.Run("JSON with all fields", func(t *testing.T) {
		configPath := "/tmp/all_fields.json"
		jsonContent := `{
			"server_address": "10.0.0.1:8000",
			"base_url": "https://short.example",
			"file_storage_path": "/tmp/all_events.log",
			"database_dsn": "postgres://user:pass@localhost/db",
			"enable_https": false,
			"cert_file": "",
			"key_file": "",
			"audit_file": "/tmp/all_audit.log",
			"audit_url": "https://audit.example",
			"trusted_subnet": "192.168.0.0/24"
		}`
		err := os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		// Создаем временные файлы
		err = os.WriteFile("/tmp/all_events.log", []byte(""), 0666)
		require.NoError(t, err)
		err = os.WriteFile("/tmp/all_audit.log", []byte(""), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove("/tmp/all_events.log")
		}()
		defer func() {
			_ = os.Remove("/tmp/all_audit.log")
		}()

		args := []string{"-c", configPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)

		assert.Equal(t, "10.0.0.1:8000", cfg.ListenAddr())
		assert.Equal(t, "https://short.example", cfg.ShortBaseURL().String())
		assert.Equal(t, "postgres://user:pass@localhost/db", cfg.DBDSN())
		assert.False(t, cfg.EnabledHTTPS())
		assert.Equal(t, "/tmp/all_audit.log", cfg.AuditFile())
		assert.Equal(t, "https://audit.example", cfg.AuditURL().String())
	})

	t.Run("JSON with empty strings should not override", func(t *testing.T) {
		configPath := "/tmp/empty_fields.json"
		jsonContent := `{
			"server_address": "",
			"base_url": "",
			"file_storage_path": "",
			"database_dsn": "",
			"audit_file": ""
		}`
		err := os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		args := []string{"-c", configPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)

		// Должны остаться значения по умолчанию
		assert.Equal(t, "localhost:8080", cfg.ListenAddr())
		assert.Equal(t, "http://localhost:8080/", cfg.ShortBaseURL().String())
		assert.Empty(t, cfg.DBDSN())
		assert.Empty(t, cfg.AuditFile())
	})
}

// TestConfig_EventStoragePath тестирование пути к хранилищу событий
func TestConfig_EventStoragePath(t *testing.T) {
	t.Run("Set EventStoragePath via flag -f", func(t *testing.T) {
		eventPath := "/tmp/test_events.log"
		err := os.WriteFile(eventPath, []byte(""), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(eventPath)
		}()

		args := []string{"-f", eventPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Equal(t, eventPath, cfg.EventStoragePath())
	})

	t.Run("Set EventStoragePath via ENV", func(t *testing.T) {
		eventPath := "/tmp/env_events.log"
		err := os.WriteFile(eventPath, []byte(""), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(eventPath)
		}()

		mockEnv := map[string]string{"FILE_STORAGE_PATH": eventPath}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}
		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)
		assert.Equal(t, eventPath, cfg.EventStoragePath())
	})

	t.Run("Empty EventStoragePath from flag", func(t *testing.T) {
		args := []string{"-f", ""}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Empty(t, cfg.EventStoragePath())
	})
}

// TestConfig_AuditFile тестирование пути к файлу аудита
func TestConfig_AuditFileFull(t *testing.T) {
	t.Run("Set AuditFile via flag --audit-file", func(t *testing.T) {
		auditPath := "/tmp/test_audit.log"
		err := os.WriteFile(auditPath, []byte(""), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(auditPath)
		}()

		args := []string{"--audit-file", auditPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Equal(t, auditPath, cfg.AuditFile())
	})

	t.Run("Set AuditFile via ENV", func(t *testing.T) {
		auditPath := "/tmp/env_audit.log"
		err := os.WriteFile(auditPath, []byte(""), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(auditPath)
		}()

		mockEnv := map[string]string{"AUDIT_FILE": auditPath}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}
		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)
		assert.Equal(t, auditPath, cfg.AuditFile())
	})

	t.Run("Empty AuditFile from flag", func(t *testing.T) {
		args := []string{"--audit-file", ""}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Empty(t, cfg.AuditFile())
	})
}

// TestConfig_DBDSNPriority тестирует приоритет установки RepoDrv
func TestConfig_DBDSNPriority(t *testing.T) {
	t.Run("DBDSN set via flag sets RepoDrv to PgDB", func(t *testing.T) {
		args := []string{"-d", "postgres://localhost"}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Equal(t, "PgDB", cfg.RepoDrv())
		assert.Equal(t, "postgres://localhost", cfg.DBDSN())
	})

	t.Run("DBDSN set via ENV sets RepoDrv to PgDB", func(t *testing.T) {
		mockEnv := map[string]string{"DATABASE_DSN": "postgres://env"}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}
		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)
		assert.Equal(t, "PgDB", cfg.RepoDrv())
		assert.Equal(t, "postgres://env", cfg.DBDSN())
	})

	t.Run("No DBDSN keeps InMemory repo", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)
		assert.Equal(t, "InMemory", cfg.RepoDrv())
		assert.Empty(t, cfg.DBDSN())
	})
}

// TestConfig_NilCmdArgs тестирует передачу nil в NewConfig
// TestConfig_NilCmdArgs тестирует передачу nil в NewConfig
// TestConfig_NilCmdArgs тестирует передачу nil в NewConfig
func TestConfig_NilCmdArgs(t *testing.T) {
	t.Run("NewConfig with nil cmdArgs should not panic and use defaults", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)
		// Проверяем значения по умолчанию из config.go
		assert.Equal(t, "localhost:8080", cfg.ListenAddr())
		assert.Equal(t, "0.0.1", cfg.Version())
		assert.Equal(t, "InMemory", cfg.RepoDrv())
		assert.Empty(t, cfg.DBDSN())
		assert.Equal(t, byte(64), cfg.ShardSize())
		assert.Equal(t, "chi", cfg.RouterType())
	})

	t.Run("NewConfig with empty slice should also use defaults", func(t *testing.T) {
		emptyArgs := []string{}
		cfg, err := NewConfig(&emptyArgs, nil)
		require.NoError(t, err)
		assert.Equal(t, "localhost:8080", cfg.ListenAddr())
		assert.Equal(t, "0.0.1", cfg.Version())
	})
}

// TestConfig_TrustedSubnet тестирует работу с доверенной подсетью
func TestConfig_TrustedSubnet(t *testing.T) {
	t.Run("Set and get TrustedSubnet", func(t *testing.T) {
		cfg := Config{}
		prefix, err := netip.ParsePrefix("192.168.1.0/24")
		require.NoError(t, err)

		newCfg := cfg.SetTrustedSubnet(&prefix)
		assert.Equal(t, &prefix, newCfg.TrustedSubnet())
	})

	t.Run("Set nil TrustedSubnet", func(t *testing.T) {
		cfg := Config{}
		newCfg := cfg.SetTrustedSubnet(nil)
		assert.Nil(t, newCfg.TrustedSubnet())
	})

	t.Run("TrustedSubnet from JSON", func(t *testing.T) {
		configPath := "/tmp/test_trusted.json"
		jsonContent := `{"trusted_subnet": "10.0.0.0/8"}`
		err := os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		args := []string{"-c", configPath}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)

		expected, _ := netip.ParsePrefix("10.0.0.0/8")
		assert.Equal(t, &expected, cfg.TrustedSubnet())
	})

	t.Run("TrustedSubnet from ENV", func(t *testing.T) {
		mockEnv := map[string]string{"TRUSTED_SUBNET": "172.16.0.0/12"}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}

		cfg, err := NewConfig(nil, lookup)
		require.NoError(t, err)

		expected, _ := netip.ParsePrefix("172.16.0.0/12")
		assert.Equal(t, &expected, cfg.TrustedSubnet())
	})

	t.Run("TrustedSubnet from flag -t", func(t *testing.T) {
		args := []string{"-t", "192.168.0.0/16"}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)

		expected, _ := netip.ParsePrefix("192.168.0.0/16")
		assert.Equal(t, &expected, cfg.TrustedSubnet())
	})

	t.Run("Invalid TrustedSubnet in ENV", func(t *testing.T) {
		mockEnv := map[string]string{"TRUSTED_SUBNET": "invalid-cidr"}
		lookup := func(k string) (string, bool) {
			val, ok := mockEnv[k]
			return val, ok
		}

		_, err := NewConfig(nil, lookup)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid ENV TRUSTED_SUBNET")
	})

	t.Run("Invalid TrustedSubnet in JSON", func(t *testing.T) {
		configPath := "/tmp/invalid_trusted.json"
		jsonContent := `{"trusted_subnet": "not-a-cidr"}`
		err := os.WriteFile(configPath, []byte(jsonContent), 0666)
		require.NoError(t, err)
		defer func() {
			_ = os.Remove(configPath)
		}()

		args := []string{"-c", configPath}
		_, err = NewConfig(&args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "trusted_subnet invalid format")
	})

	t.Run("Invalid TrustedSubnet in flag -t", func(t *testing.T) {
		args := []string{"-t", "invalid"}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid trusted subnet CIDR")
	})
}
