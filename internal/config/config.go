package config

import "flag"

type Config struct {
	listen              string
	shortURLHostAndPort string
}

func GetConfig() *Config {
	c := Config{}
	flag.StringVar(&c.listen, "listen", "localhost:8080", "IP address and port that the server listens to [IP-address]:portnumber")
	flag.StringVar(&c.shortURLHostAndPort, "shortURLHostAndPort", "localhost:8080", "Host name and port that the server returns in short URL [IP-address]:portnumber")
	flag.Parse()
	return &c
}

func (c *Config) Listen() string {
	return c.listen
}

func (c *Config) ShortURLHostAndPort() string {
	return c.shortURLHostAndPort
}
