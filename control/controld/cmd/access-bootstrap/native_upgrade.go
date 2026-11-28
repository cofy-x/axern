package main

import (
	"context"
	"errors"
	"flag"
	"os"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgadmin "github.com/cofy-x/axern/control/controld/internal/postgres/admin"
)

func runNativeUpgradeCheck(ctx context.Context, args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("native upgrade requires the root maintenance operator")
	}
	flags := flag.NewFlagSet("native-upgrade-check", flag.ContinueOnError)
	current := flags.String("node-id", "", "the stopped installation's Node")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *current == "" {
		return errors.New("native upgrade requires the original Node identity")
	}
	db, err := postgres.Open(ctx, os.Getenv("CONTROLD_POSTGRES_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.CheckUpgradeMigrations(ctx); err != nil {
		return err
	}
	return pgadmin.NewStore(db).RequireNativeWorkDrained(ctx, *current)
}
