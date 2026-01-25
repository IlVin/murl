package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	c1 := GetConfig().Modify(map[string]string{"Listen": "321"})
	c2 := c1.Modify(map[string]string{"Listen": "123"})
	assert.Equal(t, "321", c1.Listen())
	assert.Equal(t, "123", c2.Listen())
}
