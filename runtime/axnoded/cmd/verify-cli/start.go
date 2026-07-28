package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

const defaultCreateSandboxTimeout = 90 * time.Second

func runVerifyCLI(cfg verifyCLIConfig) error {
	clients, err := verifyutil.DialNodeClients(cfg.address)
	if err != nil {
		return fmt.Errorf("dial axnoded: %w", err)
	}
	defer clients.Close()

	rootfsSpec, err := verifyutil.BuildRootfsSpec(
		cfg.rootfsSrc,
		cfg.rootfsPath,
		cfg.imageURL,
		cfg.s3Endpoint,
		cfg.s3Bucket,
		cfg.s3Object,
		cfg.s3AccessKeyID,
		cfg.s3AccessKeySecret,
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
		RuntimeClass: cfg.runtime,
		Argv:         []string{"/bin/sh", "-c", cfg.shellCommand},
		Cwd:          "/",
		Env:          userEnvs,
		Mounts:       mounts,
		Resources:    startResources,
		StdoutPath:   cfg.stdoutPath,
		StderrPath:   cfg.stderrPath,
	}
	rootfsSpec.Apply(spec)

	handle, err := verifyutil.CreateAllocationWithAttempt(ctx, clients, verifyutil.NewSandboxID(cfg.runtimeID), cfg.allocationAttempt, spec)
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}

	fmt.Printf("container_id=%s\n", handle.SandboxID)
	fmt.Printf("allocation_attempt=%d\n", handle.Attempt)
	return nil
}
