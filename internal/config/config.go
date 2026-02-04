package config

import (
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"

	"go.uber.org/zap"
)

// +------------------+
// |    IZapLogger    |
// +------------------+
// Интерфейс для быстрого эмбеддинга
type IZapLogger interface {
	Zap() *zap.Logger
}

// +--------------------------+
// |  Config - иммутабельный  |
// +--------------------------+
type Config struct {
	zap                      *zap.Logger
	version                  string
	listenAddr               SocketAddr
	shortBaseURL             ShortBaseURL
	routerType               string
	repoDrv                  string
	shardSize                byte
	compressibleContentTypes map[string]struct{}
}

type LookupEnvFunc func(key string) (string, bool)

// NewConfig фабрика конфига, которая должна вызываться один раз
func NewConfig(cmdArgs *[]string, lookupEnv LookupEnvFunc, zapLogger *zap.Logger) (*Config, error) {
	// Подмена функции чтения переменных окружения
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	// Default config
	cfg := Config{
		zap:          zapLogger,
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
	}

	// Command line arguments
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
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
	if cmdArgs != nil {
		if err := fs.Parse(*cmdArgs); err != nil {
			return &cfg, fmt.Errorf("failed to parse flags: %w", err)
		}
	}

	// Парсинг переменных окружения
	if val, ok := lookupEnv("SERVER_ADDRESS"); ok {
		addr, err := NewSocketAddr(val)
		if err != nil {
			return &cfg, fmt.Errorf("env SERVER_ADDRESS error: %w", err)
		}
		cfg = cfg.SetListenAddr(addr)
	}
	if val, ok := lookupEnv("BASE_URL"); ok {
		sb, err := NewShortBaseURL(val)
		if err != nil {
			return &cfg, fmt.Errorf("env BASE_URL error: %w", err)
		}
		cfg = cfg.SetShortBaseURL(sb)
	}

	return &cfg, nil
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

func (c Config) Zap() *zap.Logger {
	return c.zap
}
func (c Config) SetZap(zapLogger *zap.Logger) Config {
	c.zap = zapLogger
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

// =================  UTILS  =================

// Must враппер, поддерживающий цепочки изменения конфига
// Например: newCfg := oldCfg.SetListenAddr(Must(NewSocketAddr("127.0.0.1:45"))).SetVersion("123")
func Must[T any](val T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("config panic: %v", err))
	}
	return val
}
