package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgadmin "github.com/cofy-x/axern/control/controld/internal/postgres/admin"
	"github.com/cofy-x/axern/internal/proto/nativebackup"
)

func runNativeRestore(ctx context.Context, command string, args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("native restore requires the root maintenance operator")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	source := flags.String("old-node", "", "the fenced original Node")
	replacement := flags.String("new-node", "", "the replacement Node")
	planPath := flags.String("plan", "", "private verified mapping returned by the storage owner")
	tokenPath := flags.String("enrollment-token-file", "", "fresh replacement Node token file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *source == "" || *replacement == "" {
		return errors.New("native restore requires explicit source and replacement Nodes")
	}
	db, err := postgres.Open(ctx, os.Getenv("CONTROLD_POSTGRES_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.CheckMigrations(ctx); err != nil {
		return err
	}
	store := pgadmin.NewStore(db)
	if command == "native-restore-plan" {
		if *planPath != "" || *tokenPath != "" {
			return errors.New("planning requires only the old and new Node identities")
		}
		plan, err := store.PlanNativeRestore(ctx, *source, *replacement)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	if *planPath == "" || *tokenPath == "" {
		return errors.New("committing native restore requires the verified mapping and a fresh token")
	}
	file, err := os.Open(*planPath)
	if err != nil {
		return err
	}
	defer file.Close()
	plan, err := nativebackup.Decode(file, true)
	if err != nil {
		return err
	}
	if plan.OldNode != *source || plan.NewNode != *replacement {
		return errors.New("native restore plan does not match the explicit Node identities")
	}
	token, err := os.ReadFile(*tokenPath)
	if err != nil {
		return err
	}
	return store.CommitNativeRestore(ctx, plan, string(token), time.Now().UTC())
}
