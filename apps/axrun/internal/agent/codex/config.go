package codex

import (
	"os"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
)

type Config struct {
	ConfigPath string
	Profiles   map[string]agent.Profile
}

func ConfigFromEnv() (Config, error) {
	config := Config{
		ConfigPath: strings.TrimSpace(os.Getenv("AXERN_CONFIG")),
	}
	return config, nil
}
