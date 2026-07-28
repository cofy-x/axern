package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/app"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/config"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/lib/go/observability/logrusotel"
	"github.com/sirupsen/logrus"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gatewayd: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		return err
	}
	if err := configureLogging(cfg.LogLevel); err != nil {
		return err
	}
	obs, err := sdkobs.Init(context.Background(), sdkobs.ConfigFromEnv(
		sdkobs.WithServiceName("gatewayd"),
		sdkobs.WithComponent("gatewayd"),
	))
	if err != nil {
		return err
	}
	if obs.Enabled() {
		logrus.AddHook(logrusotel.New("gatewayd"))
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
	svc, err := app.New(ctx, cfg, obs)
	if err != nil {
		return err
	}
	defer svc.Close()
	return svc.Run(ctx)
}

func configureLogging(levelName string) error {
	level, err := logrus.ParseLevel(strings.ToLower(levelName))
	if err != nil {
		return err
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	return nil
}
