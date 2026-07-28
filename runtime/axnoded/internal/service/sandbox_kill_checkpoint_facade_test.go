package service

import (
	"context"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestKill(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	})

	containerID := "axctl-test-kill"
	s.containerManager.StoreMetadata(containerID, &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: "runsc",
	})
	time.Sleep(200 * time.Millisecond)

	resp, err := s.Kill(context.Background(), &runtime.KillRequest{
		ID:     containerID,
		Signal: "sigterm",
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, handler.killCalls)
	if assert.NotNil(t, handler.lastKillRequest) {
		assert.Equal(t, containerID, handler.lastKillRequest.GetID())
		assert.Equal(t, "TERM", handler.lastKillRequest.GetSignal())
	}
	assert.Equal(t, containerID, handler.lastOptions.ContainerID)
}

func TestKillRejectsExitedContainer(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	})

	containerID := "axctl-test-kill-exited"
	s.containerManager.StoreMetadata(containerID, &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: "runsc",
	})
	time.Sleep(200 * time.Millisecond)

	c, err := s.containerManager.Get(containerID)
	assert.NoError(t, err)
	err = c.Status.UpdateSync(func(st container.Status) (container.Status, error) {
		st.FinishedAt = time.Now().Format(time.RFC3339Nano)
		st.ExitCode = 0
		st.ExitCodeKnown = true
		return st, nil
	})
	assert.NoError(t, err)

	resp, err := s.Kill(context.Background(), &runtime.KillRequest{ID: containerID})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, 0, handler.killCalls)
}

func TestCheckpoint_ContainerNotFound(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	resp, err := s.Checkpoint(context.Background(), &runtime.CheckpointRequest{
		ID:      "axctl-nonexistent",
		CkptDir: "/tmp/ckpt",
	})
	assert.Nil(t, err)
	assert.False(t, resp.Success)
}
