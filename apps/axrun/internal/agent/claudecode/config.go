package claudecode

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
)

type Config struct {
	CWD            string
	User           string
	TimeoutSec     int
	MaxTurns       int
	OutputFormat   string
	AllowedTools   []string
	IdleTimeoutSec int
	Env            map[string]string
	ConfigPath     string
	Profiles       map[string]agent.Profile
}

func ConfigFromEnv() (Config, error) {
	config := Config{
		CWD:          strings.TrimSpace(firstNonEmpty(os.Getenv("AXRUN_CLAUDE_CODE_CWD"), os.Getenv("AXRUN_AGENT_CWD"))),
		User:         strings.TrimSpace(firstNonEmpty(os.Getenv("AXRUN_CLAUDE_CODE_USER"), os.Getenv("AXRUN_AGENT_USER"))),
		OutputFormat: strings.TrimSpace(firstNonEmpty(os.Getenv("AXRUN_CLAUDE_CODE_OUTPUT_FORMAT"), os.Getenv("AXRUN_AGENT_OUTPUT_FORMAT"))),
		ConfigPath:   strings.TrimSpace(os.Getenv("AXERN_CONFIG")),
	}
	timeoutText := strings.TrimSpace(firstNonEmpty(os.Getenv("AXRUN_CLAUDE_CODE_TIMEOUT_SEC"), os.Getenv("AXRUN_AGENT_TIMEOUT_SEC")))
	if timeoutText != "" {
		timeout, err := strconv.Atoi(timeoutText)
		if err != nil || timeout < 1 {
			return Config{}, fmt.Errorf("AXRUN_CLAUDE_CODE_TIMEOUT_SEC must be a positive integer")
		}
		config.TimeoutSec = timeout
	}
	maxTurnsText := strings.TrimSpace(firstNonEmpty(os.Getenv("AXRUN_CLAUDE_CODE_MAX_TURNS"), os.Getenv("AXRUN_AGENT_MAX_TURNS")))
	if maxTurnsText != "" {
		maxTurns, err := strconv.Atoi(maxTurnsText)
		if err != nil || maxTurns < 0 {
			return Config{}, fmt.Errorf("AXRUN_CLAUDE_CODE_MAX_TURNS must be a non-negative integer")
		}
		config.MaxTurns = maxTurns
	}
	idleTimeoutText := strings.TrimSpace(firstNonEmpty(os.Getenv("AXRUN_CLAUDE_CODE_IDLE_TIMEOUT_SEC"), os.Getenv("AXRUN_AGENT_IDLE_TIMEOUT_SEC")))
	if idleTimeoutText != "" {
		idleTimeout, err := strconv.Atoi(idleTimeoutText)
		if err != nil || idleTimeout < 0 {
			return Config{}, fmt.Errorf("AXRUN_CLAUDE_CODE_IDLE_TIMEOUT_SEC must be a non-negative integer")
		}
		config.IdleTimeoutSec = idleTimeout
	}
	config.AllowedTools = parseCSV(firstNonEmpty(os.Getenv("AXRUN_CLAUDE_CODE_ALLOWED_TOOLS"), os.Getenv("AXRUN_AGENT_ALLOWED_TOOLS")))
	env, err := parseEnvList(firstNonEmpty(os.Getenv("AXRUN_CLAUDE_CODE_ENV"), os.Getenv("AXRUN_AGENT_ENV")))
	if err != nil {
		return Config{}, err
	}
	config.Env = env
	return config, nil
}

func parseCSV(value string) []string {
	var result []string
	for item := range strings.SplitSeq(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseEnvList(value string) (map[string]string, error) {
	env := map[string]string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, val, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("agent env entries must use KEY=VALUE")
		}
		env[key] = val
	}
	return env, nil
}
