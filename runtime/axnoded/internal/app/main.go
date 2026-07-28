package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/lib/go/observability/logrusotel"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/sirupsen/logrus"
)

type options struct {
	rootDir     string
	configPath  string
	socketPath  string
	grpcAddress string
	httpAddress string
	logLevel    string
	logFile     string
}

func Run() error {
	opts, err := parseFlags()
	if err != nil {
		return err
	}
	if err := configureLogging(opts.logLevel, opts.logFile); err != nil {
		return err
	}
	if opts.configPath == "" {
		opts.configPath = filepath.Join(opts.rootDir, "config.toml")
	}
	if err := os.MkdirAll(opts.rootDir, 0755); err != nil {
		return fmt.Errorf("create root directory: %w", err)
	}
	cfg, err := loadSandboxConfig(opts.configPath)
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	nodeID := cfg.PluginConfig.ControlPlaneNodeIDValue(hostname)
	obs, err := sdkobs.Init(context.Background(), sdkobs.ConfigFromEnv(
		sdkobs.WithServiceName("axnoded"),
		sdkobs.WithComponent("axnoded"),
		sdkobs.WithNodeID(nodeID),
	))
	if err != nil {
		return err
	}
	if obs.Enabled() {
		logrus.AddHook(logrusotel.New("axnoded"))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := obs.Shutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Warn("shutdown OpenTelemetry")
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return serve(ctx, opts, cfg, obs)
}

func parseFlags() (options, error) {
	opts := options{}
	flagSet := flagSet()
	flagSet.StringVar(&opts.rootDir, "root", config.DefaultRootDir, "axnoded working root directory")
	flagSet.StringVar(&opts.configPath, "config", "", "path to axnoded TOML config")
	flagSet.StringVar(&opts.socketPath, "socket", config.DefaultSocketAddress, "axnoded gRPC unix socket")
	flagSet.StringVar(&opts.grpcAddress, "grpc-address", "", "axnoded node gRPC TCP listen address")
	flagSet.StringVar(&opts.httpAddress, "http-address", config.DefaultHttpAddress, "axnoded HTTP listen address")
	flagSet.StringVar(&opts.logLevel, "log-level", "info", "log level: debug|info|warn|error")
	flagSet.StringVar(&opts.logFile, "log-file", "", "log file path; stderr is used when empty")
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		return options{}, err
	}
	return opts, nil
}

func flagSet() *flag.FlagSet {
	return flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

func configureLogging(levelName, logFile string) error {
	level, err := logrus.ParseLevel(strings.ToLower(levelName))
	if err != nil {
		return fmt.Errorf("parse log level %q: %w", levelName, err)
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	if logFile == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", logFile, err)
	}
	logrus.SetOutput(file)
	return nil
}
