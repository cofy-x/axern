package main

import (
	"context"
	"fmt"
	"time"

	privatenodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
)

const defaultCreateSandboxTimeout = 90 * time.Second

func runVerifyCLI(cfg verifyCLIConfig) error {
	clients, err := verifyutil.DialNodeClients(cfg.address)
	if err != nil {
		return fmt.Errorf("dial axnoded: %w", err)
	}
	defer clients.Close()
	if cfg.deleteAllocationID != "" {
		deleteTimeout := cfg.deleteTimeout
		if deleteTimeout <= 0 {
			deleteTimeout = 2 * time.Minute
		}
		ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
		defer cancel()
		return verifyutil.DeleteAllocation(ctx, clients, cfg.deleteAllocationID, 0)
	}

	rootfsSpec, err := verifyutil.BuildRootfsSpec(
		cfg.rootfsSrc,
		cfg.rootfsPath,
		cfg.imageURL,
	)
	if err != nil {
		return fmt.Errorf("build rootfs spec: %w", err)
	}

	startResources := buildStartResources(cfg)
	userEnvs, mounts, err := buildDynamicOptions(cfg)
	if err != nil {
		return err
	}

	createTimeout := cfg.createTimeout
	if createTimeout <= 0 {
		createTimeout = defaultCreateSandboxTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), createTimeout)
	defer cancel()

	spec := &privatenodev1.ResolvedExecutionConfig{
		Argv:       []string{"/bin/sh", "-c", cfg.shellCommand},
		Cwd:        "/",
		Env:        userEnvs,
		Mounts:     mounts,
		Resources:  startResources,
		StdoutPath: cfg.stdoutPath,
		StderrPath: cfg.stderrPath,
	}
	rootfsSpec.Apply(spec)

	allocationID := verifyutil.NewSandboxID(cfg.environmentID)
	handle, err := verifyutil.CreateAllocation(ctx, clients, allocationID, spec)
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}

	fmt.Printf("container_id=%s\n", handle.SandboxID)
	return nil
}
