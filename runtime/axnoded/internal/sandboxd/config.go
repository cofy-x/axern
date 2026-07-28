package sandboxd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
)

const (
	DefaultSocketPath      = "/mnt/axern-sandboxd.sock"
	DefaultShutdownTimeout = 10 * time.Second
)

type Config struct {
	SocketPath      string
	EntrypointJSON  string
	ShutdownTimeout time.Duration
	Entrypoint      workload.Entrypoint
}

func ParseConfig(args []string, stderr io.Writer) (Config, error) {
	cfg := Config{
		SocketPath:      DefaultSocketPath,
		ShutdownTimeout: DefaultShutdownTimeout,
	}
	flags := flag.NewFlagSet("axern-sandboxd", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&cfg.SocketPath, "socket", DefaultSocketPath, "Unix socket path for the sandbox daemon API")
	flags.StringVar(&cfg.EntrypointJSON, "entrypoint-json", "", "path to structured entrypoint metadata")
	flags.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", DefaultShutdownTimeout, "graceful shutdown timeout")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	trailingArgs := flags.Args()
	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("shutdown-timeout must be positive")
	}
	if strings.TrimSpace(cfg.SocketPath) == "" {
		return Config{}, fmt.Errorf("socket path is required")
	}
	if cfg.EntrypointJSON != "" && len(trailingArgs) > 0 {
		return Config{}, fmt.Errorf("entrypoint-json and trailing argv are mutually exclusive")
	}
	if cfg.EntrypointJSON != "" {
		entrypoint, err := loadEntrypoint(cfg.EntrypointJSON)
		if err != nil {
			return Config{}, err
		}
		cfg.Entrypoint = entrypoint
	} else if len(trailingArgs) > 0 {
		cfg.Entrypoint.Args = append([]string(nil), trailingArgs...)
	}
	if err := validateEntrypoint(cfg.Entrypoint); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadEntrypoint(path string) (workload.Entrypoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return workload.Entrypoint{}, fmt.Errorf("read entrypoint metadata: %w", err)
	}
	var entrypoint workload.Entrypoint
	if err := json.Unmarshal(data, &entrypoint); err != nil {
		return workload.Entrypoint{}, fmt.Errorf("parse entrypoint metadata: %w", err)
	}
	return entrypoint, nil
}

func validateEntrypoint(entrypoint workload.Entrypoint) error {
	for _, item := range entrypoint.Env {
		name, _, ok := strings.Cut(item, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("entrypoint env values must use KEY=VALUE format")
		}
	}
	if len(entrypoint.Args) > 0 && strings.TrimSpace(entrypoint.Args[0]) == "" {
		return fmt.Errorf("entrypoint args must start with a non-empty executable")
	}
	return nil
}
