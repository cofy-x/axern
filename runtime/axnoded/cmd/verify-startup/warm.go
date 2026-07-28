package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func runVerifyStartup(cfg verifyStartupConfig) error {
	if strings.TrimSpace(cfg.runtimeID) == "" {
		cfg.runtimeID = fmt.Sprintf("startup-matrix-%s", strings.ReplaceAll(strings.TrimSpace(cfg.scenario), "/", "-"))
	}
	if cfg.samples <= 0 {
		return fmt.Errorf("samples must be greater than zero")
	}

	if err := os.MkdirAll(cfg.stdoutDir, 0755); err != nil {
		return fmt.Errorf("create stdout dir: %w", err)
	}
	if err := os.MkdirAll(cfg.stderrDir, 0755); err != nil {
		return fmt.Errorf("create stderr dir: %w", err)
	}

	rootfsConfig, rootfsKey, rootfsType, err := buildRootfsConfig(cfg.rootfsSrc, cfg.rootfsPath, cfg.imageURL, cfg.s3Endpoint, cfg.s3Bucket, cfg.s3Object, cfg.s3AccessKeyID, cfg.s3AccessKeySecret)
	if err != nil {
		return fmt.Errorf("build rootfs config: %w", err)
	}
	command, err := resolveCommand(cfg.argvJSON)
	if err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}
	conn, err := verifyutil.DialNodeClients(cfg.address)
	if err != nil {
		return fmt.Errorf("dial axnoded: %w", err)
	}
	defer conn.Close()

	startedAt := time.Now().UTC()
	if strings.TrimSpace(cfg.mode) == "warm" {
		if err := primeWarmRuntime(conn, rootfsConfig, cfg.runtimeName, cfg.runtimeID, command, cfg.stdoutDir, cfg.stderrDir, cfg.omitStdio, cfg.waitBeforeDelete, cfg.expectedExit); err != nil {
			return fmt.Errorf("prime warm runtime: %w", err)
		}
	}

	before, err := natbench.CaptureStartupSnapshot(cfg.metricsURL, cfg.runtimeName, rootfsType)
	if err != nil {
		return fmt.Errorf("capture startup metrics before samples: %w", err)
	}
	for i := 0; i < cfg.samples; i++ {
		if err := startAndDelete(
			conn,
			rootfsConfig,
			cfg.runtimeName,
			fmt.Sprintf("%s-%02d", cfg.runtimeID, i+1),
			command,
			filepathJoin(cfg.stdoutDir, fmt.Sprintf("%s-%02d.stdout", cfg.runtimeID, i+1)),
			filepathJoin(cfg.stderrDir, fmt.Sprintf("%s-%02d.stderr", cfg.runtimeID, i+1)),
			cfg.omitStdio,
			cfg.waitBeforeDelete,
			cfg.expectedExit,
		); err != nil {
			return fmt.Errorf("start/delete sample %d: %w", i+1, err)
		}
	}
	after, err := natbench.CaptureStartupSnapshot(cfg.metricsURL, cfg.runtimeName, rootfsType)
	if err != nil {
		return fmt.Errorf("capture startup metrics after samples: %w", err)
	}
	startupSummary := natbench.DiffStartupSummary(before, after)
	localitySummary, err := natbench.CaptureLocalitySummary(cfg.inventoryURL, rootfsKey)
	if err != nil {
		return fmt.Errorf("capture locality summary: %w", err)
	}

	report := natbench.StartupScenarioSampleReport{
		Scenario:    strings.TrimSpace(cfg.scenario),
		Runtime:     cfg.runtimeName,
		RootfsType:  rootfsType,
		MountType:   strings.TrimSpace(cfg.mountType),
		RootfsKey:   rootfsKey,
		Mode:        strings.TrimSpace(cfg.mode),
		Samples:     cfg.samples,
		Startup:     startupSummary,
		Locality:    localitySummary,
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return fmt.Errorf("encode startup sample report: %w", err)
	}
	return nil
}

func primeWarmRuntime(clients *verifyutil.NodeClients, rootfsConfig *verifyutil.RootfsSpec, runtimeName, runtimeID string, command []string, stdoutDir, stderrDir string, omitStdio bool, waitBeforeDelete bool, expectedExit int) error {
	return startAndDelete(clients, rootfsConfig, runtimeName, runtimeID+"-prime", command, filepathJoin(stdoutDir, runtimeID+"-prime.stdout"), filepathJoin(stderrDir, runtimeID+"-prime.stderr"), omitStdio, waitBeforeDelete, expectedExit)
}

func startAndDelete(clients *verifyutil.NodeClients, rootfsConfig *verifyutil.RootfsSpec, runtimeName, sandboxID string, command []string, stdoutPath, stderrPath string, omitStdio bool, waitBeforeDelete bool, expectedExit int) error {
	startCtx, cancelStart := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelStart()
	spec := &privatenodev1.ResolvedExecutionConfig{
		RuntimeClass: runtimeName,
		Argv:         append([]string(nil), command...),
		Cwd:          "/",
	}
	if !omitStdio {
		spec.StdoutPath = stdoutPath
		spec.StderrPath = stderrPath
	}
	rootfsConfig.Apply(spec)

	handle, err := verifyutil.CreateAllocation(startCtx, clients, verifyutil.NewSandboxID(sandboxID), spec)
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	if waitBeforeDelete {
		waitCtx, cancelWait := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancelWait()
		waitResp, err := handle.Wait(waitCtx)
		if err != nil {
			return fmt.Errorf("wait sandbox: %w", err)
		}
		if waitResp.GetExitCode() != int32(expectedExit) {
			return fmt.Errorf("unexpected exit code: %d", waitResp.GetExitCode())
		}
	}

	deleteCtx, cancelDelete := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelDelete()
	if err := handle.Delete(deleteCtx, 0); err != nil {
		if grpcstatus.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("delete sandbox: %w", err)
	}
	return nil
}
