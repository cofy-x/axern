package app

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/cofy-x/axern/internal/proto/nativebackup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/workspace"
)

func restoreRetainedWorkspaces(opts options) error {
	if os.Geteuid() != 0 || opts.configPath == "" || opts.nativeRestorePlan == "" {
		return errors.New("native restore requires root, an explicit configuration and a restore plan")
	}
	cfg, err := loadSandboxConfig(opts.configPath)
	if err != nil {
		return err
	}
	if !cfg.RuntimeConfig.NativeRecovery {
		return errors.New("native restore requires the managed native profile")
	}
	input, err := os.Open(opts.nativeRestorePlan)
	if err != nil {
		return err
	}
	defer input.Close()
	plan, err := nativebackup.Decode(input, opts.nativeRestoreApply)
	if err != nil {
		return err
	}
	mapped, err := workspace.NewFromConfig(cfg).RemapRetained(plan, opts.nativeRestoreApply)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(mapped)
}
