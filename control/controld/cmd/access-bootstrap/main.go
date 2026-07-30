package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgaccess "github.com/cofy-x/axern/control/controld/internal/postgres/access"
)

const bootstrapTimeout = 60 * time.Second

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), bootstrapTimeout)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("controld-access-bootstrap", flag.ContinueOnError)
	dsn := flags.String("postgres-dsn", os.Getenv("CONTROLD_POSTGRES_DSN"), "PostgreSQL DSN")
	name := flags.String("principal-name", "platform-admin", "initial platform administrator name")
	displayName := flags.String("display-name", "Platform Administrator", "initial platform administrator display name")
	certificatePath := flags.String("certificate", "", "initial platform administrator certificate PEM path")
	rolloutWorkerCertificatePath := flags.String("rollout-worker-certificate", "", "managed rollout worker certificate PEM path")
	label := flags.String("credential-label", "bootstrap-admin", "initial credential label")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	der, err := readCertificateDER(*certificatePath)
	if err != nil {
		return err
	}
	fingerprint, notAfter, err := accesskernel.ParseCertificateDER(der)
	if err != nil {
		return err
	}
	db, err := postgres.Open(ctx, *dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.CheckMigrations(ctx); err != nil {
		return err
	}
	store := pgaccess.NewStore(db)
	now := time.Now().UTC()
	if err := store.BootstrapPlatformAdmin(ctx, strings.TrimSpace(*name), strings.TrimSpace(*displayName), strings.TrimSpace(*label), fingerprint, notAfter, now); err != nil {
		return err
	}
	if strings.TrimSpace(*rolloutWorkerCertificatePath) == "" {
		return errors.New("rollout worker certificate path is required")
	}
	rolloutDER, err := readCertificateDER(*rolloutWorkerCertificatePath)
	if err != nil {
		return err
	}
	rolloutFingerprint, rolloutNotAfter, err := accesskernel.ParseCertificateDER(rolloutDER)
	if err != nil {
		return err
	}
	return store.BootstrapRolloutExecutor(ctx, rolloutFingerprint, rolloutNotAfter, now)
}

func readCertificateDER(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("certificate path is required")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bootstrap certificate: %w", err)
	}
	block, _ := pem.Decode(contents)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("bootstrap certificate must be PEM encoded")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse bootstrap certificate: %w", err)
	}
	return certificate.Raw, nil
}
