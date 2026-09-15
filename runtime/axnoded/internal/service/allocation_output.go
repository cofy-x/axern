package service

import (
	"context"
	"errors"
	"os"
	"time"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *sandboxService) ReadAllocationOutput(ctx context.Context, id, cursor string) ([]allocationoutput.Chunk, bool, error) {
	// Hold the lifecycle lock through the bounded read, not merely source selection.
	// Teardown cannot remove live files between selecting and opening them.
	unlock := h.allocationController().LockAllocationLifecycle(id)
	defer unlock()
	return allocationoutput.NewWithSources(h.allocationOutputSources).Read(ctx, id, cursor)
}

func (h *sandboxService) allocationOutputSources(ctx context.Context, id string) (allocationoutput.Sources, error) {
	retained, err := allocationoutput.NewRetention(h.config.RootDir).Sources(id, time.Now())
	if err == nil {
		return retained, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return allocationoutput.Sources{}, status.Error(codes.Internal, "allocation output metadata is invalid")
	}
	list, err := h.List(ctx, &runtimev1.ListContainersRequest{ID: id})
	if err != nil {
		return allocationoutput.Sources{}, err
	}
	if len(list.GetContainers()) != 1 {
		return allocationoutput.Sources{}, status.Error(codes.NotFound, "allocation output is unavailable or expired")
	}
	c := list.GetContainers()[0]
	return allocationoutput.Sources{Stdout: c.GetStdout(), Stderr: c.GetStderr(), Terminal: c.GetState() == runtimev1.ContainerState_CONTAINER_EXITED}, nil
}

func (h *sandboxService) startOutputRetention(ctx context.Context) error {
	retention := allocationoutput.NewRetention(h.config.RootDir)
	if err := retention.Sweep(time.Now(), true); err != nil {
		logrus.WithError(err).Error("recover retained Allocation output; invalid output remains unreadable")
	}
	ctx, h.outputRetentionCancel = context.WithCancel(ctx)
	h.outputRetentionWG.Add(1)
	go func() {
		defer h.outputRetentionWG.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := retention.Sweep(now, false); err != nil {
					logrus.WithError(err).Error("expire Allocation output")
				}
			}
		}
	}()
	return nil
}
