package sandboxd

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/execflow"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type processClient interface {
	StartProcess(context.Context, ProcessStartRequest) (ProcessStatus, error)
	WaitProcess(context.Context, string) (ProcessStatus, error)
}

var newProcessClient = func(socketPath string) processClient {
	return NewClient(socketPath)
}

func ExecContainer(ctx context.Context, request *apipb.ExecContainerRequest, options contract.HandlerOptions, containerRoot string) (*apipb.ExecContainerResponse, error) {
	socketPath, err := processSocketPath(containerRoot, options, request.GetTty(), request.GetManagedProxy() != nil)
	if err != nil {
		return nil, err
	}
	client := newProcessClient(socketPath)
	started, err := client.StartProcess(ctx, ProcessStartRequest{
		Args:          request.GetCommand(),
		Cwd:           request.GetCwd(),
		Env:           processEnvList(execflow.KeyValueMap(request.GetEnvs())),
		User:          request.GetUser(),
		CaptureOutput: true,
		Terminal:      request.GetTty(),
		ManagedProxy:  managedProxySpec(request.GetManagedProxy()),
	})
	if err != nil {
		return nil, processOperationError("start exec process", err)
	}
	status, err := client.WaitProcess(ctx, started.ID)
	if err != nil {
		return nil, processOperationError("wait exec process", err)
	}
	exitCode, err := processExitCode(status)
	if err != nil {
		return nil, err
	}
	return &apipb.ExecContainerResponse{
		ExitCode:           int32(exitCode),
		Stdout:             []byte(status.Stdout),
		Stderr:             []byte(status.Stderr),
		StdoutTruncated:    status.StdoutTruncated,
		StderrTruncated:    status.StderrTruncated,
		ManagedProxyReport: managedProxyReport(status.ManagedProxyReport),
	}, nil
}

func processEnvList(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key, value := range values {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

func processSocketPath(containerRoot string, options contract.HandlerOptions, terminal bool, managedProxy bool) (string, error) {
	containerID := options.ContainerID
	if strings.TrimSpace(containerID) == "" {
		return "", fmt.Errorf("sandboxd process requires container id: %w", errord.ErrInvalidArgument)
	}
	if strings.TrimSpace(containerRoot) == "" {
		return "", fmt.Errorf("sandboxd process requires container root: %w", errord.ErrFailedPrecondition)
	}
	if err := requireCapabilityFromLabels(options.ContainerLabels, wire.CapabilityProcess); err != nil {
		return "", err
	}
	if terminal {
		if err := requireCapabilityFromLabels(options.ContainerLabels, wire.CapabilityPTY); err != nil {
			return "", err
		}
	}
	if managedProxy {
		if err := requireCapabilityFromLabels(options.ContainerLabels, wire.CapabilityManagedProxy); err != nil {
			return "", err
		}
	}
	return runtimeoci.SandboxdBundleSocketPath(filepath.Join(containerRoot, containerID)), nil
}

func processOperationError(operation string, err error) error {
	return OperationError(wire.CapabilityProcess, operation, err)
}

func processExitCode(status ProcessStatus) (int, error) {
	if status.ExitCode != nil {
		return *status.ExitCode, nil
	}
	if status.LastError != "" {
		return 0, fmt.Errorf("sandboxd process %s did not report exit code: %s: %w", status.ID, status.LastError, errord.ErrFailedPrecondition)
	}
	return 0, fmt.Errorf("sandboxd process %s did not report exit code: %w", status.ID, errord.ErrFailedPrecondition)
}
