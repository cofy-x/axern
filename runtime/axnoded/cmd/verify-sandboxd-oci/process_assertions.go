package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func assertSandboxdProcessAPI(ctx context.Context, bundlePath string) error {
	client := runtimesandboxd.NewClient(runtimeoci.SandboxdBundleSocketPath(bundlePath))
	started, err := client.StartProcess(ctx, runtimesandboxd.ProcessStartRequest{
		Args:          []string{"/bin/sh", "-c", "printf '%s:%s:%s' \"$AXERN_PROC_E2E\" \"$(pwd)\" \"$(cat)\""},
		Cwd:           "/tmp",
		Env:           []string{"AXERN_PROC_E2E=ok"},
		Stdin:         "stdin",
		CaptureOutput: true,
	})
	if err != nil {
		return fmt.Errorf("start process: %w", err)
	}
	if err := assertProcessListContains(ctx, client, started.ID); err != nil {
		return err
	}
	waited, err := client.WaitProcess(ctx, started.ID)
	if err != nil {
		return fmt.Errorf("wait process: %w", err)
	}
	if waited.ExitCode == nil || *waited.ExitCode != 0 || strings.TrimSpace(waited.Stdout) != "ok:/tmp:stdin" {
		return fmt.Errorf("process wait status = %#v", waited)
	}
	if err := assertConcurrentProcesses(ctx, client); err != nil {
		return err
	}
	if err := assertSignalProcess(ctx, client); err != nil {
		return err
	}
	if err := assertTimedProcess(ctx, client); err != nil {
		return err
	}
	return assertKillProcess(ctx, client)
}

func assertProcessListContains(ctx context.Context, client *runtimesandboxd.Client, id string) error {
	list, err := client.ListProcesses(ctx)
	if err != nil {
		return fmt.Errorf("list processes: %w", err)
	}
	for _, status := range list.Processes {
		if status.ID == id {
			return nil
		}
	}
	return fmt.Errorf("process list missing %s: %#v", id, list)
}

func assertConcurrentProcesses(ctx context.Context, client *runtimesandboxd.Client) error {
	var ids []string
	for i := 0; i < 3; i++ {
		status, err := client.StartProcess(ctx, runtimesandboxd.ProcessStartRequest{
			Args:          []string{"/bin/sh", "-c", "exit 0"},
			CaptureOutput: true,
		})
		if err != nil {
			return fmt.Errorf("start concurrent process %d: %w", i, err)
		}
		ids = append(ids, status.ID)
	}
	for _, id := range ids {
		status, err := client.WaitProcess(ctx, id)
		if err != nil {
			return fmt.Errorf("wait concurrent process %s: %w", id, err)
		}
		if status.ExitCode == nil || *status.ExitCode != 0 {
			return fmt.Errorf("concurrent process status = %#v", status)
		}
	}
	return nil
}

func assertKillProcess(ctx context.Context, client *runtimesandboxd.Client) error {
	sleeping, err := client.StartProcess(ctx, runtimesandboxd.ProcessStartRequest{
		Args: []string{"/bin/sleep", "30"},
	})
	if err != nil {
		return fmt.Errorf("start kill process: %w", err)
	}
	if _, err := client.SignalProcess(ctx, sleeping.ID, "KILL"); err != nil {
		return fmt.Errorf("kill process: %w", err)
	}
	killed, err := client.WaitProcess(ctx, sleeping.ID)
	if err != nil {
		return fmt.Errorf("wait killed process: %w", err)
	}
	if killed.ExitCode == nil || *killed.ExitCode != 137 {
		return fmt.Errorf("killed process status = %#v", killed)
	}
	return nil
}

func assertSignalProcess(ctx context.Context, client *runtimesandboxd.Client) error {
	sleeping, err := client.StartProcess(ctx, runtimesandboxd.ProcessStartRequest{
		Args: []string{"/bin/sleep", "30"},
	})
	if err != nil {
		return fmt.Errorf("start signal process: %w", err)
	}
	if _, err := client.SignalProcess(ctx, sleeping.ID, "TERM"); err != nil {
		return fmt.Errorf("signal process: %w", err)
	}
	signaled, err := client.WaitProcess(ctx, sleeping.ID)
	if err != nil {
		return fmt.Errorf("wait signaled process: %w", err)
	}
	if signaled.ExitCode == nil || *signaled.ExitCode != 143 {
		return fmt.Errorf("signaled process status = %#v", signaled)
	}
	return nil
}

func assertTimedProcess(ctx context.Context, client *runtimesandboxd.Client) error {
	sleeping, err := client.StartProcess(ctx, runtimesandboxd.ProcessStartRequest{
		Args:      []string{"/bin/sleep", "30"},
		TimeoutMs: 50,
	})
	if err != nil {
		return fmt.Errorf("start timed process: %w", err)
	}
	timed, err := client.WaitProcess(ctx, sleeping.ID)
	if err != nil {
		return fmt.Errorf("wait timed process: %w", err)
	}
	if timed.ExitCode == nil || *timed.ExitCode != 137 || !strings.Contains(timed.LastError, "process timed out after") {
		return fmt.Errorf("timed process status = %#v", timed)
	}
	return nil
}

func assertSandboxdBackedExecContainer(ctx context.Context, cfg config, bundlePath string) error {
	socketPath := runtimeoci.SandboxdBundleSocketPath(bundlePath)
	snapshot, err := runtimesandboxd.NewClient(socketPath).WaitReady(ctx, runtimesandboxd.DefaultReadyTimeout, runtimesandboxd.DefaultPollInterval)
	if err != nil {
		return fmt.Errorf("wait sandboxd ready for runtime exec: %w", err)
	}
	labels := runtimesandboxd.EnrichLabels(nil, socketPath, snapshot)
	containerRoot := filepath.Dir(bundlePath)
	runtimeRoot := filepath.Dir(containerRoot)
	handler, err := newVerifyRuntimeHandlerWithRoot(cfg, runtimeRoot)
	if err != nil {
		return err
	}
	response, err := handler.ExecContainer(ctx, &apipb.ExecContainerRequest{
		Command: []string{"/bin/sh", "-c", "printf '%s:%s' \"$AXERN_EXEC_E2E\" \"$(pwd)\""},
		Envs:    []*apipb.KeyValue{{Key: "AXERN_EXEC_E2E", Value: "ok"}},
		Cwd:     "/tmp",
	}, contract.HandlerOptions{
		ContainerID:     filepath.Base(bundlePath),
		ContainerLabels: labels,
	})
	if err != nil {
		return fmt.Errorf("sandboxd-backed ExecContainer: %w", err)
	}
	if response.GetExitCode() != 0 || strings.TrimSpace(string(response.GetStdout())) != "ok:/tmp" {
		return fmt.Errorf("sandboxd-backed ExecContainer response = %#v", response)
	}
	return nil
}

func assertSandboxdBackedExecSession(ctx context.Context, cfg config, bundlePath string) error {
	socketPath := runtimeoci.SandboxdBundleSocketPath(bundlePath)
	snapshot, err := runtimesandboxd.NewClient(socketPath).WaitReady(ctx, runtimesandboxd.DefaultReadyTimeout, runtimesandboxd.DefaultPollInterval)
	if err != nil {
		return fmt.Errorf("wait sandboxd ready for runtime exec session: %w", err)
	}
	labels := runtimesandboxd.EnrichLabels(nil, socketPath, snapshot)
	containerRoot := filepath.Dir(bundlePath)
	runtimeRoot := filepath.Dir(containerRoot)
	handler, err := newVerifyRuntimeHandlerWithRoot(cfg, runtimeRoot)
	if err != nil {
		return err
	}
	session, err := handler.OpenExecSession(ctx, &apipb.ExecSessionOpen{
		Command: []string{"/bin/sh", "-c", "cat; printf ':err' >&2"},
	}, contract.HandlerOptions{
		ContainerID:     filepath.Base(bundlePath),
		ContainerLabels: labels,
	})
	if err != nil {
		return fmt.Errorf("sandboxd-backed OpenExecSession: %w", err)
	}
	defer session.Close()
	if err := session.Write([]byte("ok")); err != nil {
		return fmt.Errorf("sandboxd-backed session stdin: %w", err)
	}
	if err := session.CloseStdin(); err != nil {
		return fmt.Errorf("sandboxd-backed session close stdin: %w", err)
	}
	var stdout, stderr strings.Builder
	for {
		chunk, err := session.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("sandboxd-backed session recv: %w", err)
		}
		stdout.Write(chunk.Stdout)
		stderr.Write(chunk.Stderr)
	}
	exit, err := session.Wait()
	if err != nil {
		return fmt.Errorf("sandboxd-backed session wait: %w", err)
	}
	if exit.Status != 0 || stdout.String() != "ok" || stderr.String() != ":err" {
		return fmt.Errorf("sandboxd-backed session exit=%#v stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	return nil
}
