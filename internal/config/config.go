// Package config управляет конфигурацией приложения.
//
// Поддерживает иерархическую систему приоритетов: значения по умолчанию,
// аргументы командной строки и переменные окружения. Структура Config спроектирована
// как иммутабельная — любые изменения возвращают новую копию объекта.
package config

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/spf13/pflag"
)

// Config представляет собой иммутабельную структуру конфигурации приложения.
// Для изменения значений используйте методы Set*, которые возвращают обновленную копию.
type Config struct {
	version                  string
	listenAddr               SocketAddr
	shortBaseURL             ShortBaseURL
	routerType               string
	repoDrv                  string
	shardSize                byte
	compressibleContentTypes map[string]struct{}
	eventStoragePath         string
	dbConfigPath             string
	dbDSN                    string
	maxBodySize              int64
	jwtSecretKey             string
	jwtTTL                   time.Duration
	keySession               KeySession
	auditFile                string
	auditURL                 *url.URL
	enabledHTTPS             bool
	certFile                 string
	keyFile                  string
	configFile               string
}

// KeySession — тип-обертка для ключа сессии в контексте или куках.
type KeySession string

// LookupEnvFunc — тип функции для поиска переменных окружения (используется для тестирования).
type LookupEnvFunc func(key string) (string, bool)

// NewConfig — фабрика для создания конфигурации.
// Реализует приоритетность: значения по умолчанию -> флаги -> переменные окружения.
// Если в процессе парсинга указаны пути к файлам (audit или event storage), проверяет их доступность.
func NewConfig(cmdArgs *[]string, lookupEnv LookupEnvFunc) (Config, error) {
	// Подмена функции чтения переменных окружения
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	// Default config
	cfg := Config{
		version:      "0.0.1",
		listenAddr:   SocketAddr{hostname: "localhost", port: "8080"},
		shortBaseURL: ShortBaseURL{url.URL{Scheme: "http", Host: "localhost:8080", Path: "/"}},
		routerType:   "chi",
		repoDrv:      "InMemory",
		shardSize:    64,
		compressibleContentTypes: map[string]struct{}{
			"application/json":       {},
			"text/html":              {},
			"text/css":               {},
			"application/javascript": {},
		},
		eventStoragePath: "",
		dbConfigPath:     "",
		dbDSN:            "",
		maxBodySize:      1024 * 1024,
		jwtSecretKey:     "jwtSecretKey+jwtSecretKey-jwtSecretKey+jwtSecretKey-jwtSecretKey",
		jwtTTL:           30 * 24 * time.Hour,
		keySession:       "session",
		auditFile:        "",
		auditURL:         nil,
		enabledHTTPS:     false,
		certFile:         "",
		keyFile:          "",
		configFile:       "",
	}

	// JSON Config
	if err := cfg.readJSONConfig(cmdArgs, lookupEnv); err != nil {
		return cfg, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Command line arguments
	if err := cfg.readCmdArgs(cmdArgs); err != nil {
		return cfg, fmt.Errorf("failed to parse command line args: %w", err)
	}

	// Парсинг переменных окружения
	if err := cfg.readEnv(lookupEnv); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func (cfg *Config) readJSONConfig(cmdArgs *[]string, lookupEnv LookupEnvFunc) error {
	if cmdArgs == nil || len(*cmdArgs) == 0 {
		return nil
	}

	fs := pflag.NewFlagSet("json", pflag.ContinueOnError)
	fs.ParseErrorsAllowlist.UnknownFlags = true
	fs.SetInterspersed(true)
	fs.SetOutput(io.Discard)

	fs.FuncP("config", "c", fmt.Sprintf("Path to config file (%s)", cfg.ConfigFile()), func(s string) error {
		if s != "" {
			if err := canOpenFile(s); err != nil {
				return fmt.Errorf("invalid path to config file: %w", err)
			}
			*cfg = cfg.SetConfigFile(s)
		}
		return nil
	})

	if err := fs.Parse(*cmdArgs); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if s, ok := lookupEnv("CONFIG"); ok {
		if s != "" {
			if err := canOpenFile(s); err != nil {
				return fmt.Errorf("invalid ENV CONFIG: %w", err)
			}
			*cfg = cfg.SetConfigFile(s)
		}
	}

	return nil
}

func (cfg *Config) readEnv(lookupEnv LookupEnvFunc) error {
	if val, ok := lookupEnv("DATABASE_DSN"); ok {
		*cfg = cfg.SetDBDSN(val)
	}
	if val, ok := lookupEnv("SERVER_ADDRESS"); ok {
		addr, err := NewSocketAddr(val)
		if err != nil {
			return fmt.Errorf("env SERVER_ADDRESS error: %w", err)
		}
		*cfg = cfg.SetListenAddr(addr)
	}
	if val, ok := lookupEnv("BASE_URL"); ok {
		sb, err := NewShortBaseURL(val)
		if err != nil {
			return fmt.Errorf("env BASE_URL error: %w", err)
		}
		*cfg = cfg.SetShortBaseURL(sb)
	}
	if s, ok := lookupEnv("FILE_STORAGE_PATH"); ok {
		if s != "" {
			if err := canOpenOrCreateFile(s); err != nil {
				return fmt.Errorf("env FILE_STORAGE_PATH error: invalid path to event storage: %w", err)
			}
			*cfg = cfg.SetEventStoragePath(s)
		}
	}
	if s, ok := lookupEnv("AUDIT_FILE"); ok {
		if s != "" {
			if err := canOpenOrCreateFile(s); err != nil {
				return fmt.Errorf("invalid ENV AUDIT_FILE: %w", err)
			}
			*cfg = cfg.SetAuditFile(s)
		}
	}
	if s, ok := lookupEnv("AUDIT_URL"); ok {
		if s != "" {
			u, err := url.Parse(s)
			if err != nil {
				return fmt.Errorf("invalid ENV AUDIT_URL: %w", err)
			}
			*cfg = cfg.SetAuditURL(u)
		}
	}
	if s, ok := lookupEnv("ENABLE_HTTPS"); ok {
		if s == "" || s == "0" || s == "false" {
			*cfg = cfg.SetEnabledHTTPS(false)
		} else {
			*cfg = cfg.SetEnabledHTTPS(true)
		}
	}
	if s, ok := lookupEnv("CERT_FILE"); ok {
		if s != "" {
			if err := canOpenFile(s); err != nil {
				return fmt.Errorf("invalid ENV CERT_FILE: %w", err)
			}
			*cfg = cfg.SetCertFile(s)
		}
	}
	if s, ok := lookupEnv("KEY_FILE"); ok {
		if s != "" {
			if err := canOpenFile(s); err != nil {
				return fmt.Errorf("invalid ENV KEY_FILE: %w", err)
			}
			*cfg = cfg.SetKeyFile(s)
		}
	}
	if cfg.DBDSN() != "" {
		*cfg = cfg.SetRepoDrv("PgDB")
	}
	if s, ok := lookupEnv("CONFIG"); ok {
		if s != "" {
			if err := canOpenFile(s); err != nil {
				return fmt.Errorf("invalid ENV CONFIG: %w", err)
			}
			*cfg = cfg.SetConfigFile(s)
		}
	}

	return nil
}

func (cfg *Config) readCmdArgs(cmdArgs *[]string) error {
	if cmdArgs == nil || len(*cmdArgs) == 0 {
		return nil
	}

	fs := pflag.NewFlagSet("config", pflag.ContinueOnError)
	fs.ParseErrorsAllowlist.UnknownFlags = true
	fs.SetInterspersed(true)
	fs.SetOutput(io.Discard)

	fs.FuncP("addr", "a", fmt.Sprintf("HTTP server address (%s)", cfg.ListenAddr()), func(s string) error {
		sAddr, err := NewSocketAddr(s)
		if err != nil {
			return fmt.Errorf("invalid ListenAddr format: %w", err)
		}
		*cfg = cfg.SetListenAddr(sAddr)
		return nil
	})

	fs.FuncP("dsn", "d", fmt.Sprintf("DB DSN (%s)", cfg.DBDSN()), func(s string) error {
		*cfg = cfg.SetDBDSN(s)
		return nil
	})

	fs.FuncP("enable-https", "s", fmt.Sprintf("Enabled HTTPS (%t)", cfg.EnabledHTTPS()), func(s string) error {
		if s == "" || s == "0" || s == "false" {
			*cfg = cfg.SetEnabledHTTPS(false)
		} else {
			*cfg = cfg.SetEnabledHTTPS(true)
		}
		return nil
	})

	fs.Func("cert-file", fmt.Sprintf("Path to HTTPS certificate file (%s)", cfg.CertFile()), func(s string) error {
		if s != "" {
			if err := canOpenFile(s); err != nil {
				return fmt.Errorf("invalid path to HTTPS certificate file: %w", err)
			}
			*cfg = cfg.SetCertFile(s)
		}
		return nil
	})

	fs.Func("key-file", fmt.Sprintf("Path to HTTPS key file (%s)", cfg.KeyFile()), func(s string) error {
		if s != "" {
			if err := canOpenFile(s); err != nil {
				return fmt.Errorf("invalid path to HTTPS key file: %w", err)
			}
			*cfg = cfg.SetKeyFile(s)
		}
		return nil
	})

	fs.FuncP("base-address", "b", fmt.Sprintf("Base address for short URL (%s)", cfg.ShortBaseURL()), func(s string) error {
		sbURL, err := NewShortBaseURL(s)
		if err != nil {
			return fmt.Errorf("invalid ShortBaseURL format: %w", err)
		}
		*cfg = cfg.SetShortBaseURL(sbURL)
		return nil
	})

	fs.FuncP("file-storeage", "f", fmt.Sprintf("Path to Event storage file (%s)", cfg.EventStoragePath()), func(s string) error {
		if s != "" {
			if err := canOpenOrCreateFile(s); err != nil {
				return fmt.Errorf("invalid path to event storage: %w", err)
			}
			*cfg = cfg.SetEventStoragePath(s)
		}
		return nil
	})

	fs.Func("audit-file", fmt.Sprintf("Path to audit file (%s)", cfg.AuditFile()), func(s string) error {
		if s != "" {
			if err := canOpenOrCreateFile(s); err != nil {
				return fmt.Errorf("invalid path to audit file: %w", err)
			}
			*cfg = cfg.SetAuditFile(s)
		}
		return nil
	})

	fs.Func("audit-url", fmt.Sprintf("Audit URL (%s)", cfg.AuditURL()), func(s string) error {
		if s == "" {
			return nil
		}
		u, err := url.Parse(s)
		if err != nil {
			return fmt.Errorf("invalid --audit-url: %w", err)
		}
		*cfg = cfg.SetAuditURL(u)
		return nil
	})

	if err := fs.Parse(*cmdArgs); err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	return nil
}

// JWTTTL возвращает время жизни JWT токена.
func (c Config) JWTTTL() time.Duration {
	return c.jwtTTL
}

// SetJWTTTL устанавливает время жизни JWT токена и возвращает обновленный конфиг.
func (c Config) SetJWTTTL(jwtTTL time.Duration) Config {
	c.jwtTTL = jwtTTL
	return c
}

// KeySession возвращает ключ, используемый для идентификации сессии.
func (c Config) KeySession() KeySession {
	return c.keySession
}

// SetKeySession устанавливает ключ сессии и возвращает обновленный конфиг.
func (c Config) SetKeySession(keySession KeySession) Config {
	c.keySession = keySession
	return c
}

// JWTSecretKey возвращает секретный ключ для подписи JWT.
func (c Config) JWTSecretKey() string {
	return c.jwtSecretKey
}

// SetJWTSecretKey устанавливает секретный ключ для JWT и возвращает обновленный конфиг.
func (c Config) SetJWTSecretKey(jwtSecretKey string) Config {
	c.jwtSecretKey = jwtSecretKey
	return c
}

// MaxBodySize возвращает максимально допустимый размер тела HTTP-запроса в байтах.
func (c Config) MaxBodySize() int64 {
	return c.maxBodySize
}

// SetMaxBodySize устанавливает лимит размера тела запроса и возвращает обновленный конфиг.
func (c Config) SetMaxBodySize(maxBodySize int64) Config {
	c.maxBodySize = maxBodySize
	return c
}

// DBDSN возвращает строку подключения к базе данных (Data Source Name).
func (c Config) DBDSN() string {
	return c.dbDSN
}

// SetDBDSN устанавливает DSN базы данных и возвращает обновленный конфиг.
func (c Config) SetDBDSN(dbDSN string) Config {
	c.dbDSN = dbDSN
	return c
}

// DBConfigPath возвращает путь к файлу расширенной конфигурации БД.
func (c Config) DBConfigPath() string {
	return c.dbConfigPath
}

// SetDBConfigPath устанавливает путь к конфигу БД и возвращает обновленный конфиг.
func (c Config) SetDBConfigPath(dbConfigPath string) Config {
	c.dbConfigPath = dbConfigPath
	return c
}

// EventStoragePath возвращает путь к файлу для хранения событий (в InMemory режиме).
func (c Config) EventStoragePath() string {
	return c.eventStoragePath
}

// SetEventStoragePath устанавливает путь к файлу событий и возвращает обновленный конфиг.
func (c Config) SetEventStoragePath(eventStoragePath string) Config {
	c.eventStoragePath = eventStoragePath
	return c
}

// AuditFile возвращает путь к локальному файлу аудита.
func (c Config) AuditFile() string {
	return c.auditFile
}

// SetAuditFile устанавливает путь к файлу аудита и возвращает обновленный конфиг.
func (c Config) SetAuditFile(auditFile string) Config {
	c.auditFile = auditFile
	return c
}

// AuditURL возвращает URL внешнего сервиса аудита.
func (c Config) AuditURL() *url.URL {
	return c.auditURL
}

// SetAuditURL устанавливает URL сервиса аудита и возвращает обновленный конфиг.
func (c Config) SetAuditURL(auditURL *url.URL) Config {
	c.auditURL = auditURL
	return c
}

// CompressibleContentTypes возвращает список MIME-типов, подлежащих сжатию (gzip).
func (c Config) CompressibleContentTypes() map[string]struct{} {
	return c.compressibleContentTypes
}

// SetCompressibleContentTypes устанавливает список типов для сжатия и возвращает обновленный конфиг.
func (c Config) SetCompressibleContentTypes(compressibleContentTypes map[string]struct{}) Config {
	c.compressibleContentTypes = compressibleContentTypes
	return c
}

// Version возвращает версию приложения.
func (c Config) Version() string {
	return c.version
}

// SetVersion устанавливает версию приложения и возвращает обновленный конфиг.
func (c Config) SetVersion(version string) Config {
	c.version = version
	return c
}

// RouterType возвращает тип используемого роутера (например, "chi").
func (c Config) RouterType() string {
	return c.routerType
}

// SetRouterType устанавливает тип роутера и возвращает обновленный конфиг.
func (c Config) SetRouterType(routerType string) Config {
	c.routerType = routerType
	return c
}

// RepoDrv возвращает идентификатор драйвера репозитория (например, "PgDB" или "InMemory").
func (c Config) RepoDrv() string {
	return c.repoDrv
}

// SetRepoDrv устанавливает драйвер репозитория и возвращает обновленный конфиг.
func (c Config) SetRepoDrv(repoDrv string) Config {
	c.repoDrv = repoDrv
	return c
}

// ListenAddr возвращает строковое представление адреса (host:port), на котором запустится сервер.
func (c Config) ListenAddr() string {
	return c.listenAddr.String()
}

// SetListenAddr устанавливает адрес прослушивания и возвращает обновленный конфиг.
func (c Config) SetListenAddr(listenAddr SocketAddr) Config {
	c.listenAddr = listenAddr
	return c
}

// ShortBaseURL возвращает базовый URL для формирования коротких ссылок.
func (c Config) ShortBaseURL() ShortBaseURL {
	return c.shortBaseURL
}

// SetShortBaseURL устанавливает базовый URL, принудительно удаляя из него Sensitive Data (User Info).
func (c Config) SetShortBaseURL(sb ShortBaseURL) Config {
	c.shortBaseURL.URL = sb.URL
	c.shortBaseURL.User = nil
	return c
}

// EnabledHTTPS HTTPS в веб-сервере включен
func (c Config) EnabledHTTPS() bool {
	return c.enabledHTTPS
}

// SetEnabledHTTPS включить/выключить HTTPS в веб-сервере
func (c Config) SetEnabledHTTPS(enable bool) Config {
	c.enabledHTTPS = enable
	return c
}

// CertFile возвращает путь к локальному файлу HTTPS сертификата.
func (c Config) CertFile() string {
	return c.certFile
}

// SetCertFile устанавливает путь к файлу HTTPS сертификата и возвращает обновленный конфиг.
func (c Config) SetCertFile(certFile string) Config {
	c.certFile = certFile
	return c
}

// KeyFile возвращает путь к локальному файлу HTTPS ключа.
func (c Config) KeyFile() string {
	return c.keyFile
}

// SetKeyFile устанавливает путь к файлу HTTPS ключа и возвращает обновленный конфиг.
func (c Config) SetKeyFile(keyFile string) Config {
	c.keyFile = keyFile
	return c
}

// KeyFile возвращает путь к локальному файлу HTTPS ключа.
func (c Config) ConfigFile() string {
	return c.configFile
}

// SetKeyFile устанавливает путь к файлу HTTPS ключа и возвращает обновленный конфиг.
func (c Config) SetConfigFile(configFile string) Config {
	c.configFile = configFile
	return c
}

// ShardSize возвращает количество виртуальных шардов для InMemory хранилища.
func (c Config) ShardSize() byte {
	return c.shardSize
}

// SetShardSize устанавливает размер шардирования и возвращает обновленный конфиг.
func (c Config) SetShardSize(shardSize byte) Config {
	c.shardSize = shardSize
	return c
}

// SocketAddr — структура для хранения и валидации сетевого адреса.
type SocketAddr struct {
	hostname string
	port     string
}

// NewSocketAddr создает новый SocketAddr, проверяя корректность формата host:port.
func NewSocketAddr(host string) (SocketAddr, error) {
	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		return SocketAddr{}, fmt.Errorf("bad net address: %w", err)
	}
	return SocketAddr{hostname: hostname, port: port}, nil
}

// String возвращает сетевой адрес в формате "host:port".
func (s SocketAddr) String() string {
	return net.JoinHostPort(s.hostname, s.port)
}

// ShortBaseURL — обертка над стандартным URL для работы с базовым адресом ссылок.
type ShortBaseURL struct {
	url.URL
}

// NewShortBaseURL создает ShortBaseURL из строки, проверяя её на соответствие стандарту RFC 3986.
func NewShortBaseURL(baseURL string) (ShortBaseURL, error) {
	sbURL, err := url.Parse(baseURL)
	if err != nil {
		return ShortBaseURL{}, fmt.Errorf("invalid ShortBaseURL: %w", err)
	}
	return ShortBaseURL{*sbURL}, nil
}

// String возвращает строковое представление URL.
func (s ShortBaseURL) String() string {
	return s.URL.String()
}

func canOpenOrCreateFile(path string) error {
	fh, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("cannot open or create file: %w", err)
	}
	defer func() {
		if err := fh.Close(); err != nil {
			slog.Error("close fail fail",
				slog.Any("err", err),
			)
		}
	}()
	return nil
}

func canOpenFile(path string) error {
	fh, err := os.OpenFile(path, os.O_RDONLY, 0666)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer func() {
		if err := fh.Close(); err != nil {
			slog.Error("close fail fail",
				slog.Any("err", err),
			)
		}
	}()
	return nil
}
