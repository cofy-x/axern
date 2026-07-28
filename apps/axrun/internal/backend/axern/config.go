package axern

import sandboxaxern "github.com/cofy-x/axern/apps/axrun/internal/sandbox/axern"

type Config = sandboxaxern.Config

func ConfigFromEnv() Config {
	return sandboxaxern.ConfigFromEnv()
}
