package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var ErrBadAddressFormat = errors.New("bad address format [localhost]")
var ErrBadPortFormat = errors.New("bad port format [8080]")
var ErrBadShortBaseURLFormat = errors.New("bad address format [http://host:port/]")

// NetAddress
type NetAddress struct {
	Host string
	Port int
}

func (n *NetAddress) String() string {
	return fmt.Sprintf("%s:%d", n.Host, n.Port)
}

func (n *NetAddress) Set(flagValue string) error {
	hostPort := strings.SplitN(flagValue, ":", 2)
	if len(hostPort) != 2 {
		return ErrBadAddressFormat
	}
	n.Host = hostPort[0]
	port, err := strconv.Atoi(hostPort[1])
	if err != nil {
		return errors.Join(ErrBadPortFormat, err)
	}
	n.Port = port
	return nil
}

// ShortBaseURL
type ShortBaseURL struct {
	u *url.URL
}

func (s *ShortBaseURL) String() string {
	return s.u.String()
}

func (s *ShortBaseURL) Set(flagValue string) error {
	u, err := url.Parse(string(flagValue))
	if err != nil {
		return errors.Join(ErrBadShortBaseURLFormat, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.Join(ErrBadShortBaseURLFormat, err)
	}

	s.u = u
	return nil
}

// Флаги по умолчанию
func defaultListen() *NetAddress {
	return &NetAddress{Host: "localhost", Port: 8080}
}
func defaultShortBaseURL() *ShortBaseURL {
	return &ShortBaseURL{u: &url.URL{Scheme: "http", Host: "localhost:8080"}}
}

// Функции, возвращающие значения флагов
var GetCmdFlagListen func() string = func() string { return defaultListen().String() }
var GetCmdFlagShortBaseURL func() string = func() string { return defaultShortBaseURL().String() }
var GetCmdFlagDBConfigPath func() string = func() string { return "" }

// Декларируем флаги
func InitFlags() {
	GetCmdFlagListen = func() func() string {
		listen := defaultListen()
		flag.Var(listen, "a", "Listen ["+listen.String()+"]")
		return func() string {
			return listen.String()
		}
	}()
	GetCmdFlagShortBaseURL = func() func() string {
		shortBaseURL := defaultShortBaseURL()
		flag.Var(shortBaseURL, "b", "Short base URL ["+shortBaseURL.String()+"]")
		return func() string {
			return shortBaseURL.String()
		}
	}()
	GetCmdFlagDBConfigPath = func() func() string {
		databaseConfigPath := ""
		flag.StringVar(&databaseConfigPath, "db", "", "DB config path []")
		return func() string {
			return databaseConfigPath
		}
	}()
}
