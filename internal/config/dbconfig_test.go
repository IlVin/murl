package config

import (
	_ "embed"

	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

//go:embed shards_config_for_tests.json
var shardsConfig string

func RunWithDBConfig(fn func(confPath string) error) error {
	tempFile, err := os.CreateTemp("", "db-config-*.json")
	if err != nil {
		return err
	}

	defer tempFile.Close()
	defer os.Remove(tempFile.Name())

	{
		_, err := tempFile.WriteString(shardsConfig)
		if err != nil {
			return err
		}
	}

	return fn(tempFile.Name())

}

func TestLoadDBConfig(t *testing.T) {

	assert.Nil(t, RunWithDBConfig(func(path string) error {
		_, err := LoadDBConfig(path)
		assert.Nil(t, err)

		return nil
	}))
}

func TestExists(t *testing.T) {

	assert.Nil(t, RunWithDBConfig(func(path string) error {
		c, err := LoadDBConfig(path)
		assert.Nil(t, err)

		// На существующем кластере показывает существование суффикса
		assert.True(t, c.Clusters["murl"].Shards.Exists("00"))
		assert.True(t, c.Clusters["murl"].Shards.Exists("63"))
		assert.False(t, c.Clusters["murl"].Shards.Exists("64"))

		// На несуществующем кластере никакой суфикс не существует
		assert.False(t, c.Clusters["murlNotExists"].Shards.Exists("64"))
		assert.False(t, c.Clusters["murlNotExists"].Shards.Exists("00"))

		return nil
	}))
}
