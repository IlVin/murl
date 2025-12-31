package config

type Config struct {
	listen              string
	shortURLHostAndPort string
}

func GetConfig() *Config {
	c := Config{
		listen:              "localhost:8080",
		shortURLHostAndPort: "localhost:8080",
	}
	return &c
}

func (c *Config) Listen() string {
	return c.listen
}

func (c *Config) ShortURLHostAndPort() string {
	return c.shortURLHostAndPort
}
