package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/sirupsen/logrus"
)

const migrateTimeout = 60 * time.Second

type options struct {
	postgresDSN string
	logLevel    string
	command     string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "controld-migrate: %v\n", err)
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
	switch opts.command {
	case "up":
		return migrateUp(opts.postgresDSN)
	default:
		return fmt.Errorf("unsupported command %q; valid commands: up", opts.command)
	}
}

func migrateUp(postgresDSN string) error {
	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()
	db, err := postgres.Open(ctx, postgresDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := db.ApplyMigrations(ctx)
	if err != nil {
		return err
	}
	logrus.WithFields(logrus.Fields{
		"applied": result.AppliedCount(),
		"skipped": result.SkippedCount(),
	}).Info("postgres schema migrations completed")
	return nil
}

func parseFlags() (options, error) {
	opts := options{command: "up"}
	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flagSet.StringVar(&opts.postgresDSN, "postgres-dsn", os.Getenv("CONTROLD_POSTGRES_DSN"), "Postgres DSN for the authoritative control-plane state store")
	flagSet.StringVar(&opts.logLevel, "log-level", "info", "log level: debug|info|warn|error")
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		return options{}, err
	}
	if flagSet.NArg() > 0 {
		opts.command = strings.TrimSpace(flagSet.Arg(0))
	}
	if strings.TrimSpace(opts.postgresDSN) == "" {
		return options{}, errors.New("postgres-dsn is required")
	}
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
