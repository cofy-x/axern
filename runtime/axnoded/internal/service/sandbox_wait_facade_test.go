package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWait(t *testing.T) {
	runscHandler := &runtimeSpyHandler{name: "runsc", waitExitCode: 1}
	altHandler := &runtimeSpyHandler{name: "alt-runtime", waitExitCode: 7}
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc":       runscHandler,
		"alt-runtime": altHandler,
	})

	containerID := "axctl-test-wait"
	s.containerManager.StoreMetadata(containerID, &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: "alt-runtime",
	})
	time.Sleep(200 * time.Millisecond)

	resp, err := s.Wait(context.Background(), &runtime.WaitRequest{
		ID: containerID,
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(7), resp.ExitCode)
}

func TestWaitReturnsUnavailableWhenExitCodeUnknown(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	handler.waitFunc = func(context.Context, contract.HandlerOptions) (contract.Exit, error) {
		return contract.Exit{}, fmt.Errorf("container exited but runtime exit status is unavailable: %w", contract.ErrExitStatusUnavailable)
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	})

	containerID := "axctl-test-wait-unknown"
	s.containerManager.StoreMetadata(containerID, &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: "runsc",
	})
	time.Sleep(200 * time.Millisecond)

	c, err := s.containerManager.Get(containerID)
	assert.NoError(t, err)
	err = c.Status.UpdateSync(func(st container.Status) (container.Status, error) {
		st.FinishedAt = time.Now().Format(time.RFC3339Nano)
		st.ExitCode = -1
		st.ExitCodeKnown = false
		st.Message = "container exited but runtime exit status is unavailable"
		return st, nil
	})
	assert.NoError(t, err)

	resp, err := s.Wait(context.Background(), &runtime.WaitRequest{ID: containerID})
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Equal(t, "container exited but runtime exit status is unavailable: exit status unavailable", resp.Message)
}

func TestWaitContinuesWhenStatusExitedButExitCodeUnknown(t *testing.T) {
	waitReady := make(chan struct{})
	handler := &runtimeSpyHandler{name: "runsc"}
	handler.waitFunc = func(_ context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
		<-waitReady
		return contract.Exit{Status: 9}, nil
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	})

	containerID := "axctl-test-wait-unknown-recover"
	s.containerManager.StoreMetadata(containerID, &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: "runsc",
	})
	time.Sleep(200 * time.Millisecond)

	c, err := s.containerManager.Get(containerID)
	assert.NoError(t, err)
	err = c.Status.UpdateSync(func(st container.Status) (container.Status, error) {
		st.FinishedAt = time.Now().Format(time.RFC3339Nano)
		st.ExitCode = -1
		st.ExitCodeKnown = false
		st.Message = "container exited but runtime exit status is unavailable"
		return st, nil
	})
	assert.NoError(t, err)

	resultCh := make(chan struct {
		resp *runtime.WaitResponse
		err  error
	}, 1)
	go func() {
		resp, err := s.Wait(context.Background(), &runtime.WaitRequest{ID: containerID})
		resultCh <- struct {
			resp *runtime.WaitResponse
			err  error
		}{resp: resp, err: err}
	}()

	time.Sleep(150 * time.Millisecond)
	close(waitReady)

	result := <-resultCh
	assert.NoError(t, result.err)
	if assert.NotNil(t, result.resp) {
		assert.Equal(t, int32(9), result.resp.ExitCode)
	}
}
