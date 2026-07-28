package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgretention "github.com/cofy-x/axern/control/controld/internal/postgres/retention"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/lib/go/observability/logrusotel"
	"github.com/sirupsen/logrus"
)

const cleanupTimeout = 30 * time.Second

type options struct {
	postgresDSN string
	logLevel    string
	once        bool
	retention   retentionkernel.Config
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "controld-retention: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseFlags()
	if err != nil {
		return err
	}
	if err := configureLogging(opts.logLevel); err != nil {
		return err
	}
	obs, err := sdkobs.Init(context.Background(), sdkobs.ConfigFromEnv(
		sdkobs.WithServiceName("controld-retention"),
		sdkobs.WithComponent("controld"),
	))
	if err != nil {
		return err
	}
	if obs.Enabled() {
		logrus.AddHook(logrusotel.New("controld-retention"))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := obs.Shutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Warn("shutdown OpenTelemetry")
		}
	}()

	db, err := postgres.Open(context.Background(), opts.postgresDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.CheckMigrations(context.Background()); err != nil {
		return err
	}

	controller := retentionkernel.NewController(pgretention.NewPGStore(db), opts.retention)
	runner := newRunner(controller, cleanupTimeout, nil)
	if opts.once {
		return runner.RunOnce(context.Background())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(controller.Config().Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_ = runner.RunOnce(context.Background())
		case <-ctx.Done():
			return nil
		}
	}
}

func parseFlags() (options, error) {
	opts := options{retention: retentionConfigFromEnv()}
	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flagSet.StringVar(&opts.postgresDSN, "postgres-dsn", os.Getenv("CONTROLD_POSTGRES_DSN"), "Postgres DSN for the authoritative control-plane state store")
	flagSet.StringVar(&opts.logLevel, "log-level", "info", "log level: debug|info|warn|error")
	flagSet.BoolVar(&opts.once, "once", false, "run one retention cleanup cycle and exit")
	flagSet.BoolVar(&opts.retention.Enabled, "retention-enabled", opts.retention.Enabled, "enable retention cleanup controller")
	flagSet.DurationVar(&opts.retention.Interval, "retention-interval", opts.retention.Interval, "retention cleanup interval")
	flagSet.IntVar(&opts.retention.BatchSize, "retention-batch-size", opts.retention.BatchSize, "maximum rows to delete per retention cleanup class per cycle")
	flagSet.DurationVar(&opts.retention.ServiceEventsTTL, "retention-service-events-ttl", opts.retention.ServiceEventsTTL, "service event retention TTL")
	flagSet.IntVar(&opts.retention.ServiceEventsKeep, "retention-service-events-keep", opts.retention.ServiceEventsKeep, "minimum service events to retain per service")
	flagSet.DurationVar(&opts.retention.TunnelEventsTTL, "retention-tunnel-events-ttl", opts.retention.TunnelEventsTTL, "tunnel session event retention TTL")
	flagSet.IntVar(&opts.retention.TunnelEventsKeep, "retention-tunnel-events-keep", opts.retention.TunnelEventsKeep, "minimum tunnel session events to retain per session")
	flagSet.DurationVar(&opts.retention.QuotaEventsTTL, "retention-quota-events-ttl", opts.retention.QuotaEventsTTL, "namespace quota admission event retention TTL")
	flagSet.DurationVar(&opts.retention.ServiceReplicasTTL, "retention-service-replicas-ttl", opts.retention.ServiceReplicasTTL, "service terminal replica retention TTL")
	flagSet.IntVar(&opts.retention.ServiceReplicasKeep, "retention-service-replicas-keep", opts.retention.ServiceReplicasKeep, "minimum terminal service replicas to retain per service")
	flagSet.DurationVar(&opts.retention.TerminalRunsTTL, "retention-terminal-runs-ttl", opts.retention.TerminalRunsTTL, "terminal run retention TTL")
	flagSet.DurationVar(&opts.retention.LeasesTTL, "retention-leases-ttl", opts.retention.LeasesTTL, "expired or revoked execution lease retention TTL")
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		return options{}, err
	}
	if strings.TrimSpace(opts.postgresDSN) == "" {
		return options{}, errors.New("postgres-dsn is required")
	}
	opts.retention = retentionkernel.NormalizeConfig(opts.retention)
	return opts, nil
}

func configureLogging(levelName string) error {
	level, err := logrus.ParseLevel(strings.ToLower(levelName))
	if err != nil {
		return fmt.Errorf("parse log level %q: %w", levelName, err)
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	return nil
}
