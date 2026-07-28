package sandboxcontrol

import (
	"context"
	"fmt"
	"strings"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxtarget"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type Options struct {
	ListContainers    func(...container.ListOption) []*container.Container
	GetContainer      func(string) (*container.Container, error)
	RuntimeCgroupPath func(string) (string, error)
	ContainerTarget   func(string) (sandboxtarget.Target, error)
	RunningTarget     func(string) (sandboxtarget.Target, error)
	CgroupDriver      func() (os2.CgroupDriver, error)
}

type Controller struct {
	options Options
}

func NewController(options Options) *Controller {
	if options.CgroupDriver == nil {
		options.CgroupDriver = os2.DefaultCgroupDriver
	}
	return &Controller{options: options}
}

func (c *Controller) List(_ context.Context, request *runtime.ListContainersRequest) (*runtime.ListContainersResponse, error) {
	var containers []*container.Container
	response := new(runtime.ListContainersResponse)
	if request.GetID() != "" {
		containers = c.options.ListContainers(container.ListFilterById(request.GetID()))
		if len(containers) == 0 {
			return response, errord.ErrNotFound
		}
	} else {
		containers = c.options.ListContainers(container.ListFilterByLabels(request.GetSelector()))
	}

	for _, item := range containers {
		if item == nil || item.Status == nil || item.Metadata == nil {
			continue
		}
		status := item.Status.Get()
		response.Containers = append(response.Containers, &runtime.ContainerStatus{
			ID:         item.Metadata.ID,
			Runtime:    item.Metadata.RuntimeHandler,
			State:      status.State(),
			StartedAt:  container.ParseTimestamp(status.StartedAt),
			FinishedAt: container.ParseTimestamp(status.FinishedAt),
			ExitCode:   status.ExitCode,
			Message:    status.Message,
			Labels:     item.Metadata.Labels,
			Stdout:     item.Metadata.Stdout,
			Stderr:     item.Metadata.Stderr,
			Pid:        int32(status.Pid),
		})
	}
	return response, nil
}

func (c *Controller) Stats(_ context.Context, request *runtime.StatsRequest) (*runtime.StatsResponse, error) {
	if request.GetID() == "" {
		return nil, errord.ErrInvalidArgument
	}
	if _, err := c.options.GetContainer(request.GetID()); err != nil {
		return nil, err
	}
	cgroupPath, err := c.options.RuntimeCgroupPath(request.GetID())
	if err != nil {
		return nil, err
	}
	cgroupDriver, err := c.options.CgroupDriver()
	if err != nil {
		return nil, fmt.Errorf("load cgroup driver failed: %v", err)
	}
	cgroup, err := cgroupDriver.Load(cgroupPath)
	if err != nil {
		return nil, fmt.Errorf("load cgroup %s failed: %v", cgroupPath, err)
	}
	stats, err := cgroup.Stats()
	if err != nil {
		return nil, fmt.Errorf("stat cgroup %s failed: %v", cgroupPath, err)
	}

	return &runtime.StatsResponse{
		CpuUsageNs:          stats.CPUUsageTotal,
		CpuKernelNs:         stats.CPUUsageKernel,
		CpuUserNs:           stats.CPUUsageUser,
		MemoryUsageBytes:    stats.MemoryUsage,
		MemoryLimitBytes:    stats.MemoryLimit,
		MemoryMaxUsageBytes: stats.MemoryMaxUsage,
	}, nil
}

func (c *Controller) Wait(ctx context.Context, request *runtime.WaitRequest) (*runtime.WaitResponse, error) {
	response := new(runtime.WaitResponse)
	target, err := c.options.ContainerTarget(request.GetID())
	if err != nil {
		return response, err
	}
	if target.Container == nil || target.Container.Metadata == nil {
		return response, errord.ErrInvalidContainer
	}

	status := target.Container.Status.Get()
	if status.State() == runtime.ContainerState_CONTAINER_EXITED && status.ExitCodeKnown {
		response.Message = status.Message
		response.ExitCode = status.ExitCode
		return response, nil
	}

	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	waitCh := make(chan waitResult, 1)
	go func() {
		exit, waitErr := target.Handler.Wait(waitCtx, contract.HandlerOptions{ContainerID: request.GetID()})
		waitCh <- waitResult{exit: exit, err: waitErr}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		status = target.Container.Status.Get()
		if status.State() == runtime.ContainerState_CONTAINER_EXITED && status.ExitCodeKnown {
			response.Message = status.Message
			response.ExitCode = status.ExitCode
			return response, nil
		}

		select {
		case result := <-waitCh:
			if result.err == nil {
				response.ExitCode = int32(result.exit.Status)
				response.Message = ""
				return response, nil
			}
			if contract.IsExitStatusUnavailable(result.err) {
				response.Message = result.err.Error()
				return response, fmt.Errorf("%s: %w", result.err.Error(), errord.ErrUnavailable)
			}
			return response, result.err
		case <-ctx.Done():
			return response, ctx.Err()
		case <-ticker.C:
		}
	}
}

type waitResult struct {
	exit contract.Exit
	err  error
}

func (c *Controller) Kill(ctx context.Context, request *runtime.KillRequest) (*runtime.KillResponse, error) {
	if request.GetID() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := c.options.RunningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	_, err = target.Handler.KillContainer(ctx, &runtime.SignalContainerRequest{
		ID:     request.GetID(),
		Signal: normalizeKillSignal(request.GetSignal()),
	}, contract.HandlerOptions{ContainerID: request.GetID()})
	if err != nil {
		return nil, err
	}
	return &runtime.KillResponse{}, nil
}

func normalizeKillSignal(signal string) string {
	normalized := strings.TrimSpace(signal)
	if normalized == "" {
		return "TERM"
	}
	if normalized[0] >= '0' && normalized[0] <= '9' {
		return normalized
	}
	normalized = strings.ToUpper(normalized)
	return strings.TrimPrefix(normalized, "SIG")
}

func (c *Controller) Checkpoint(_ context.Context, request *runtime.CheckpointRequest) (*runtime.CheckpointResponse, error) {
	target, err := c.options.ContainerTarget(request.GetID())
	if err != nil {
		return &runtime.CheckpointResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get container %v: %v", request.GetID(), err),
		}, nil
	}
	if err := target.Handler.CheckpointContainer(request); err != nil {
		return &runtime.CheckpointResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to checkpoint container %v: %v", request.GetID(), err),
		}, nil
	}
	return &runtime.CheckpointResponse{Success: true}, nil
}
