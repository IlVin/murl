package config

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig(t *testing.T) {

	t.Run("default values", func(t *testing.T) {
		cfg, err := NewConfig(nil, nil)
		require.NoError(t, err)
		assert.Equal(t, "localhost:8080", cfg.ListenAddr())
		assert.Equal(t, "http://localhost:8080/", cfg.ShortBaseURL().String())
	})

	t.Run("flags override", func(t *testing.T) {
		args := []string{"-a", "127.0.0.1:9090", "-b", "https://tst.ru"}
		cfg, err := NewConfig(&args, nil)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:9090", cfg.ListenAddr())
		assert.Equal(t, "https://tst.ru", cfg.ShortBaseURL().String())
	})

	t.Run("env override priority", func(t *testing.T) {
		args := []string{"-a", "flags:80"}
		mockEnv := func(key string) (string, bool) {
			switch key {
			case "SERVER_ADDRESS":
				return "env:99", true
			case "BASE_URL":
				return "https://env.com", true
			default:
				return "", false
			}
		}
		cfg, err := NewConfig(&args, mockEnv)
		require.NoError(t, err)
		assert.Equal(t, "env:99", cfg.ListenAddr())
		assert.Equal(t, "https://env.com", cfg.ShortBaseURL().String())
	})

	t.Run("invalid flag format", func(t *testing.T) {
		args := []string{"-a", "bad_addr"}
		_, err := NewConfig(&args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid ListenAddr format")

		argsURL := []string{"-b", "://wrong"}
		_, err = NewConfig(&argsURL, nil)
		assert.Error(t, err)
	})

	t.Run("invalid env format", func(t *testing.T) {
		mockEnvAddr := func(key string) (string, bool) {
			if key == "SERVER_ADDRESS" {
				return "no_port", true
			}
			return "", false
		}
		_, err := NewConfig(nil, mockEnvAddr)
		assert.Error(t, err)

		mockEnvURL := func(key string) (string, bool) {
			if key == "BASE_URL" {
				return "::", true
			}
			return "", false
		}
		_, err = NewConfig(nil, mockEnvURL)
		assert.Error(t, err)
	})
}

func TestConfigMutators(t *testing.T) {
	cfg := Config{}

	t.Run("immutable setters", func(t *testing.T) {
		cfg2 := cfg.SetVersion("1.1").
			SetRouterType("gin").
			SetRepoDrv("PgDB").
			SetShardSize(128)

		assert.Equal(t, "1.1", cfg2.Version())
		assert.Equal(t, "gin", cfg2.RouterType())
		assert.Equal(t, "PgDB", cfg2.RepoDrv())
		assert.Equal(t, byte(128), cfg2.ShardSize())
		assert.Empty(t, cfg.Version()) // Проверка иммутабельности
	})

	t.Run("SetShortBaseURL user stripping", func(t *testing.T) {
		u, _ := url.Parse("http://host.com")
		sb := ShortBaseURL{*u}
		cfg2 := cfg.SetShortBaseURL(sb)
		assert.Nil(t, cfg2.ShortBaseURL().User)
	})
}

func TestSocketAddr(t *testing.T) {
	t.Run("NewSocketAddr error", func(t *testing.T) {
		_, err := NewSocketAddr("no_port")
		assert.Error(t, err)
	})
}

func TestShortBaseURL(t *testing.T) {
	t.Run("String representation", func(t *testing.T) {
		sb, _ := NewShortBaseURL("http://localhost")
		assert.Equal(t, "http://localhost", sb.String())
	})
}
