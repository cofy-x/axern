package sandboxd

import (
	"bytes"
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
	GuestTaskLimit  int64
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
	flags.Int64Var(&cfg.GuestTaskLimit, "guest-task-limit", 0, "required guest task ceiling; zero preserves the development compatibility profile")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	trailingArgs := flags.Args()
	if err := validateGuestTaskLimit(cfg.GuestTaskLimit); err != nil {
		return Config{}, err
	}
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
	// The current producer preserves only a root, non-terminal primary
	// entrypoint. Reject behavior we cannot faithfully restore rather than
	// silently running a requested identity with daemon/root privileges.
	var metadata *struct {
		Args []string `json:"args"`
		Cwd  string   `json:"cwd"`
		Env  []string `json:"env"`
		User *struct {
			UID uint32 `json:"uid"`
			GID uint32 `json:"gid"`
		} `json:"user"`
		Terminal bool `json:"terminal"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return workload.Entrypoint{}, fmt.Errorf("parse entrypoint metadata: %w", err)
	}
	if metadata == nil {
		return workload.Entrypoint{}, fmt.Errorf("entrypoint metadata must be an object")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return workload.Entrypoint{}, fmt.Errorf("entrypoint metadata must contain exactly one object")
	}
	if metadata.Terminal || (metadata.User != nil && (metadata.User.UID != 0 || metadata.User.GID != 0)) {
		return workload.Entrypoint{}, fmt.Errorf("entrypoint metadata supports only root, non-terminal primary processes")
	}
	return workload.Entrypoint{Args: metadata.Args, Cwd: metadata.Cwd, Env: metadata.Env}, nil
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
