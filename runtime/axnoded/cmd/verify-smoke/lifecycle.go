package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
)

func runVerifySmoke(cfg verifySmokeConfig) error {
	clients, err := verifyutil.DialNodeClients(cfg.address)
	if err != nil {
		return fmt.Errorf("dial axnoded: %w", err)
	}
	defer clients.Close()

	spec, err := buildExecutionConfig(cfg)
	if err != nil {
		return err
	}

	startCtx, cancelStart := context.WithTimeout(context.Background(), startTimeout)
	defer cancelStart()
	handle, err := verifyutil.CreateAllocation(startCtx, clients, verifyutil.NewSandboxID(cfg.runtimeID), spec)
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	containerID := handle.SandboxID
	fmt.Printf("container_id=%s\n", containerID)

	if err := waitAndDelete(handle, cfg.expectedExit); err != nil {
		return fmt.Errorf("%w (stdout=%q stderr=%q)", err, readDiagnosticOutput(cfg.stdoutPath), readDiagnosticOutput(cfg.stderrPath))
	}
	if err := verifyOutputs(cfg.stdoutPath, cfg.stderrPath, cfg.expectStdout, cfg.expectStderr); err != nil {
		return err
	}

	fmt.Println("smoke_ok=true")
	return nil
}

func readDiagnosticOutput(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<unavailable: %v>", err)
	}
	const maxBytes = 4096
	if len(data) > maxBytes {
		data = data[:maxBytes]
	}
	return strings.TrimSpace(string(data))
}

func waitAndDelete(handle *verifyutil.SandboxHandle, expectedExit int) error {
	waitCtx, cancelWait := context.WithTimeout(context.Background(), waitTimeout)
	defer cancelWait()
	waitResp, err := handle.Wait(waitCtx)
	if err != nil {
		return fmt.Errorf("wait sandbox: %w", err)
	}
	if waitResp.GetExitCode() != int32(expectedExit) {
		return fmt.Errorf("unexpected exit code: %d", waitResp.GetExitCode())
	}

	deleteCtx, cancelDelete := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancelDelete()
	if err := handle.Delete(deleteCtx, 0); err != nil {
		return fmt.Errorf("delete sandbox: %w", err)
	}
	return nil
}
