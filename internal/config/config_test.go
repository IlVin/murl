package config

import (
	"testing"
)

func TestConfig(t *testing.T) {
	c := GetConfig()
	if c == nil {
		t.Fatalf("Cannot GetConfig")
	}
}
