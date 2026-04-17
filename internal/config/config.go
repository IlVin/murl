package config

import (
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"
)

// +--------------------------+
// |  Config - иммутабельный  |
// +--------------------------+
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
}

type KeySession string

type LookupEnvFunc func(key string) (string, bool)

// NewConfig фабрика конфига, которая должна вызываться один раз
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
	}

	// Command line arguments
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.Func("d", fmt.Sprintf("DB DSN (%s)", cfg.DBDSN()), func(s string) error {
		cfg = cfg.SetDBDSN(s)
		return nil
	})
	fs.Func("a", fmt.Sprintf("HTTP server address (%s)", cfg.ListenAddr()), func(s string) error {
		sAddr, err := NewSocketAddr(s)
		if err != nil {
			return fmt.Errorf("invalid ListenAddr format: %w", err)
		}
		cfg = cfg.SetListenAddr(sAddr)
		return nil
	})
	fs.Func("b", fmt.Sprintf("Base address for short URL (%s)", cfg.ShortBaseURL()), func(s string) error {
		sbURL, err := NewShortBaseURL(s)
		if err != nil {
			return fmt.Errorf("invalid ShortBaseURL format: %w", err)
		}
		cfg = cfg.SetShortBaseURL(sbURL)
		return nil
	})
	fs.Func("f", fmt.Sprintf("Path to Event storage file (%s)", cfg.EventStoragePath()), func(s string) error {
		if s == "" {
			return nil
		}
		fh, err := os.OpenFile(s, os.O_RDONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("invalid path to event storage: %w", err)
		}
		defer fh.Close()
		cfg = cfg.SetEventStoragePath(s)
		return nil
	})
	fs.Func("audit-file", fmt.Sprintf("Path to audit file (%s)", cfg.AuditFile()), func(s string) error {
		if s == "" {
			return nil
		}
		fh, err := os.OpenFile(s, os.O_RDONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("invalid path to audit file: %w", err)
		}
		defer fh.Close()
		cfg = cfg.SetAuditFile(s)
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
		cfg = cfg.SetAuditURL(u)
		return nil
	})
	if cmdArgs != nil {
		if err := fs.Parse(*cmdArgs); err != nil {
			return cfg, fmt.Errorf("failed to parse flags: %w", err)
		}
	}

	// Парсинг переменных окружения
	if val, ok := lookupEnv("DATABASE_DSN"); ok {
		cfg = cfg.SetDBDSN(val)
	}
	if val, ok := lookupEnv("SERVER_ADDRESS"); ok {
		addr, err := NewSocketAddr(val)
		if err != nil {
			return cfg, fmt.Errorf("env SERVER_ADDRESS error: %w", err)
		}
		cfg = cfg.SetListenAddr(addr)
	}
	if val, ok := lookupEnv("BASE_URL"); ok {
		sb, err := NewShortBaseURL(val)
		if err != nil {
			return cfg, fmt.Errorf("env BASE_URL error: %w", err)
		}
		cfg = cfg.SetShortBaseURL(sb)
	}
	if s, ok := lookupEnv("FILE_STORAGE_PATH"); ok {
		if s != "" {
			fh, err := os.OpenFile(s, os.O_RDONLY|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				return cfg, fmt.Errorf("env FILE_STORAGE_PATH error: invalid path to event storage: %w", err)
			}
			defer fh.Close()
			cfg = cfg.SetEventStoragePath(s)
		}
	}
	if s, ok := lookupEnv("AUDIT_FILE"); ok {
		if s != "" {
			fh, err := os.OpenFile(s, os.O_RDONLY|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				return cfg, fmt.Errorf("invalid ENV AUDIT_FILE: %w", err)
			}
			defer fh.Close()
			cfg = cfg.SetAuditFile(s)
		}
	}
	if s, ok := lookupEnv("AUDIT_URL"); ok {
		if s != "" {
			u, err := url.Parse(s)
			if err != nil {
				return cfg, fmt.Errorf("invalid ENV AUDIT_URL: %w", err)
			}
			cfg = cfg.SetAuditURL(u)
		}
	}

	if cfg.DBDSN() != "" {
		cfg = cfg.SetRepoDrv("PgDB")
	}
	return cfg, nil
}

func (c Config) JWTTTL() time.Duration {
	return c.jwtTTL
}
func (c Config) SetJWTTTL(jwtTTL time.Duration) Config {
	c.jwtTTL = jwtTTL
	return c
}

func (c Config) KeySession() KeySession {
	return c.keySession
}
func (c Config) SetKeySession(keySession KeySession) Config {
	c.keySession = keySession
	return c
}

func (c Config) JWTSecretKey() string {
	return c.jwtSecretKey
}
func (c Config) SetJWTSecretKey(jwtSecretKey string) Config {
	c.jwtSecretKey = jwtSecretKey
	return c
}

func (c Config) MaxBodySize() int64 {
	return c.maxBodySize
}
func (c Config) SetMaxBodySize(maxBodySize int64) Config {
	c.maxBodySize = maxBodySize
	return c
}

func (c Config) DBDSN() string {
	return c.dbDSN
}
func (c Config) SetDBDSN(dbDSN string) Config {
	c.dbDSN = dbDSN
	return c
}

func (c Config) DBConfigPath() string {
	return c.dbConfigPath
}
func (c Config) SetDBConfigPath(dbConfigPath string) Config {
	c.dbConfigPath = dbConfigPath
	return c
}

func (c Config) EventStoragePath() string {
	return c.eventStoragePath
}
func (c Config) SetEventStoragePath(eventStoragePath string) Config {
	c.eventStoragePath = eventStoragePath
	return c
}

func (c Config) AuditFile() string {
	return c.auditFile
}
func (c Config) SetAuditFile(auditFile string) Config {
	c.auditFile = auditFile
	return c
}

func (c Config) AuditURL() *url.URL {
	return c.auditURL
}
func (c Config) SetAuditURL(auditURL *url.URL) Config {
	c.auditURL = auditURL
	return c
}

func (c Config) CompressibleContentTypes() map[string]struct{} {
	return c.compressibleContentTypes
}
func (c Config) SetCompressibleContentTypes(compressibleContentTypes map[string]struct{}) Config {
	c.compressibleContentTypes = compressibleContentTypes
	return c
}

func (c Config) Version() string {
	return c.version
}
func (c Config) SetVersion(version string) Config {
	c.version = version
	return c
}

func (c Config) RouterType() string {
	return c.routerType
}
func (c Config) SetRouterType(routerType string) Config {
	c.routerType = routerType
	return c
}

func (c Config) RepoDrv() string {
	return c.repoDrv
}
func (c Config) SetRepoDrv(repoDrv string) Config {
	c.repoDrv = repoDrv
	return c
}

func (c Config) ListenAddr() string {
	return c.listenAddr.String()
}
func (c Config) SetListenAddr(listenAddr SocketAddr) Config {
	c.listenAddr = listenAddr
	return c
}

func (c Config) ShortBaseURL() ShortBaseURL {
	return c.shortBaseURL
}
func (c Config) SetShortBaseURL(sb ShortBaseURL) Config {
	c.shortBaseURL.URL = sb.URL
	c.shortBaseURL.User = nil
	return c
}

func (c Config) ShardSize() byte {
	return c.shardSize
}
func (c Config) SetShardSize(shardSize byte) Config {
	c.shardSize = shardSize
	return c
}

// +----------------------------+
// | SocketAddr - иммутабельный |
// +----------------------------+
type SocketAddr struct {
	hostname string
	port     string
}

func NewSocketAddr(host string) (SocketAddr, error) {
	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		return SocketAddr{}, fmt.Errorf("bad net address: %w", err)
	}
	return SocketAddr{hostname: hostname, port: port}, nil
}

func (s SocketAddr) String() string {
	return net.JoinHostPort(s.hostname, s.port)
}

// +------------------------------+
// | ShortBaseURL - иммутабельный |
// +------------------------------+
type ShortBaseURL struct {
	url.URL
}

func NewShortBaseURL(baseURL string) (ShortBaseURL, error) {
	sbURL, err := url.Parse(baseURL)
	if err != nil {
		return ShortBaseURL{}, fmt.Errorf("invalid ShortBaseURL: %w", err)
	}
	return ShortBaseURL{*sbURL}, nil
}

func (s ShortBaseURL) String() string {
	return s.URL.String()
}
