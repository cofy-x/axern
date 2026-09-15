package config

import (
	"bytes"
	"fmt"

	"github.com/pelletier/go-toml"
)

// Decode applies the node configuration to defaults. Unknown keys fail closed
// so removed settings and misspelled enforcement controls cannot be ignored.
func Decode(data []byte) (Config, error) {
	cfg := DefaultConfig()
	if err := toml.NewDecoder(bytes.NewReader(data)).Strict(true).Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode node configuration: %w", err)
	}
	if err := cfg.ValidateNodeIdentity(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
