package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var version string = "0.0.1"

var ErrBadAddressFormat = errors.New("bad address format [localhost]")
var ErrBadPortFormat = errors.New("bad port format [8080]")
var ErrBadShortBaseURLFormat = errors.New("bad address format [http://host:port/]")

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

type ShortBaseURL struct {
	u *url.URL
}

func (s *ShortBaseURL) String() string {
	return s.u.String()
}

func (s *ShortBaseURL) Set(flagValue string) error {
	re := regexp.MustCompile(`\A(http|https)://([^:]+)(|:([0-9]+))/?\z`)
	res := re.Find([]byte(flagValue))
	if res == nil {
		return ErrBadShortBaseURLFormat
	}

	u, err := url.Parse(string(res))
	if err != nil {
		return errors.Join(ErrBadShortBaseURLFormat, err)
	}

	s.u = u
	return nil
}

var cmdFlags = struct {
	Listen       NetAddress
	ShortBaseURL ShortBaseURL
}{
	Listen:       NetAddress{Host: "localhost", Port: 8080},
	ShortBaseURL: ShortBaseURL{u: &url.URL{Scheme: "http", Host: "localhost:8080"}},
}

func getCmdFlagListen() string {
	return cmdFlags.Listen.String()
}

func getCmdFlagShortBaseURL() string {
	return cmdFlags.ShortBaseURL.String()
}

func init() {
	flag.Var(&(cmdFlags.Listen), "a", "Listen [host:port]")
	flag.Var(&(cmdFlags.ShortBaseURL), "b", "Short base URL [http://localhost:8080/]")
}
