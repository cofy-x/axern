package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/protobuf/proto"
)

func runProbeContainer(clients *verifyutil.NodeClients, baseSpec *privatenodev1.ResolvedExecutionConfig, runtimeID, stdoutPath, stderrPath string, command []string) ([]byte, []byte, error) {
	handle, err := startProbeContainer(clients, baseSpec, runtimeID, stdoutPath, stderrPath, command)
	if err != nil {
		return nil, nil, err
	}
	defer handle.cleanup()
	return handle.wait(waitTimeout)
}

type probeContainerHandle struct {
	handle     *verifyutil.SandboxHandle
	runtimeID  string
	stdoutPath string
	stderrPath string
}

func startProbeContainer(clients *verifyutil.NodeClients, baseSpec *privatenodev1.ResolvedExecutionConfig, runtimeID, stdoutPath, stderrPath string, command []string) (*probeContainerHandle, error) {
	_ = os.Remove(stdoutPath)
	_ = os.Remove(stderrPath)

	spec := proto.Clone(baseSpec).(*privatenodev1.ResolvedExecutionConfig)
	spec.Argv = append([]string(nil), command...)
	spec.StdoutPath = stdoutPath
	spec.StderrPath = stderrPath
	spec.LinuxCapabilities = []string{"CAP_NET_RAW"}
	startCtx, cancelStart := context.WithTimeout(context.Background(), startTimeout)
	defer cancelStart()
	handle, err := verifyutil.CreateAllocation(startCtx, clients, verifyutil.NewSandboxID(runtimeID), spec)
	if err != nil {
		return nil, fmt.Errorf("create egress probe sandbox %s: %w", runtimeID, err)
	}
	return &probeContainerHandle{
		handle:     handle,
		runtimeID:  runtimeID,
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
	}, nil
}

func (h *probeContainerHandle) wait(timeout time.Duration) ([]byte, []byte, error) {
	if timeout <= 0 {
		timeout = waitTimeout
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), timeout)
	defer cancelWait()
	waitResp, err := h.handle.Wait(waitCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("wait egress probe sandbox %s: %w", h.runtimeID, err)
	}

	stdoutData, stdoutErr := os.ReadFile(h.stdoutPath)
	stderrData, stderrErr := os.ReadFile(h.stderrPath)
	if waitResp.GetExitCode() != 0 {
		return stdoutData, stderrData, fmt.Errorf(
			"unexpected egress probe exit code %d for %s (stdout=%q stderr=%q)",
			waitResp.GetExitCode(),
			h.runtimeID,
			strings.TrimSpace(string(stdoutData)),
			strings.TrimSpace(string(stderrData)),
		)
	}
	if stdoutErr != nil {
		return nil, nil, fmt.Errorf("read egress probe stdout for %s: %w", h.runtimeID, stdoutErr)
	}
	if stderrErr != nil {
		return nil, nil, fmt.Errorf("read egress probe stderr for %s: %w", h.runtimeID, stderrErr)
	}
	return stdoutData, stderrData, nil
}

func (h *probeContainerHandle) cleanup() {
	if h == nil {
		return
	}
	if h.handle != nil {
		deleteCtx, cancelDelete := context.WithTimeout(context.Background(), deleteTimeout)
		defer cancelDelete()
		_ = h.handle.Delete(deleteCtx, 0)
	}
}
