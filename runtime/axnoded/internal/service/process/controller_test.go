package process

import (
	"context"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxtarget"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControllerExecRejectsInvalidArgument(t *testing.T) {
	controller := NewController(Options{})

	_, err := controller.Exec(context.Background(), &runtime.ExecRequest{})

	assert.ErrorIs(t, err, errord.ErrInvalidArgument)
}

func TestControllerExecMapsTargetToRuntimeExec(t *testing.T) {
	handler := &controllerHandler{FakeRuntimeHandler: runtimetest.NewFakeRuntimeHandler()}
	controller := NewController(Options{
		ExecTarget: func(id string) (sandboxtarget.Target, error) {
			assert.Equal(t, "alloc-1", id)
			return testTarget("alloc-1", handler), nil
		},
	})

	_, err := controller.Exec(context.Background(), &runtime.ExecRequest{
		ID:      "alloc-1",
		Command: []string{"echo", "ok"},
		Env:     map[string]string{"A": "B"},
		Cwd:     "/workspace",
		User:    "agent",
	})

	require.NoError(t, err)
	assert.Equal(t, "alloc-1", handler.lastExecRequest.GetID())
	assert.Equal(t, []string{"echo", "ok"}, handler.lastExecRequest.GetCommand())
	assert.Equal(t, "alloc-1", handler.lastExecOptions.ContainerID)
	assert.Equal(t, map[string]string{"ready": "true"}, handler.lastExecOptions.ContainerLabels)
}

func TestControllerProcessSendsReadyAndExit(t *testing.T) {
	session := newSessionStub()
	session.exit = contract.Exit{Status: 3}
	handler := &controllerHandler{
		FakeRuntimeHandler: runtimetest.NewFakeRuntimeHandler(),
		session:            session,
	}
	controller := NewController(Options{
		ExecTarget: func(id string) (sandboxtarget.Target, error) {
			assert.Equal(t, "alloc-1", id)
			return testTarget("alloc-1", handler), nil
		},
	})
	stream := &processStreamStub{requests: []*runtime.ProcessRequest{
		{Payload: &runtime.ProcessRequest_Open{Open: &runtime.ProcessOpen{
			ID:      "alloc-1",
			Command: []string{"tool"},
			Timeout: 1,
		}}},
	}}

	err := controller.Process(stream)

	require.NoError(t, err)
	require.Len(t, stream.sent, 2)
	assert.NotNil(t, stream.sent[0].GetReady())
	assert.Equal(t, int32(3), stream.sent[1].GetExit().GetExitCode())
	assert.Equal(t, "alloc-1", handler.lastProcessOptions.ContainerID)
}

func TestControllerExecStreamRecordsTimeoutResult(t *testing.T) {
	unblockWait := make(chan struct{})
	defer close(unblockWait)
	session := newSessionStub()
	session.blockWaitCh = unblockWait
	handler := &controllerHandler{
		FakeRuntimeHandler: runtimetest.NewFakeRuntimeHandler(),
		session:            session,
	}
	controller := NewController(Options{
		ExecTarget: func(id string) (sandboxtarget.Target, error) {
			return testTarget(id, handler), nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	stream := &execStreamStub{
		ctx: ctx,
		requests: []*runtime.ExecStreamRequest{
			{Payload: &runtime.ExecStreamRequest_Open{Open: &runtime.ExecStreamOpen{
				ID:      "alloc-1",
				Command: []string{"tool"},
			}}},
		},
	}

	err := controller.ExecStream(stream)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

type controllerHandler struct {
	*runtimetest.FakeRuntimeHandler
	session            contract.Session
	lastExecRequest    *runtime.ExecContainerRequest
	lastExecOptions    contract.HandlerOptions
	lastProcessOptions contract.HandlerOptions
}

func (h *controllerHandler) ExecContainer(_ context.Context, request *apipb.ExecContainerRequest, options contract.HandlerOptions) (*apipb.ExecContainerResponse, error) {
	h.lastExecRequest = request
	h.lastExecOptions = options
	return &apipb.ExecContainerResponse{}, nil
}

func (h *controllerHandler) OpenExecSession(_ context.Context, _ *apipb.ExecSessionOpen, options contract.HandlerOptions) (contract.Session, error) {
	h.lastProcessOptions = options
	if h.session != nil {
		return h.session, nil
	}
	return newSessionStub(), nil
}

func (h *controllerHandler) ProcessService() contract.ProcessService {
	return controllerProcessService{handler: h}
}

type controllerProcessService struct {
	handler *controllerHandler
}

func (s controllerProcessService) OpenProcess(_ context.Context, _ *apipb.ProcessOpen, options contract.HandlerOptions) (contract.Session, error) {
	s.handler.lastProcessOptions = options
	if s.handler.session != nil {
		return s.handler.session, nil
	}
	return newSessionStub(), nil
}

func testTarget(id string, handler contract.RuntimeHandler) sandboxtarget.Target {
	return sandboxtarget.Target{
		ID: id,
		Metadata: &runtime.ContainerMetadata{
			ID:             id,
			RuntimeHandler: "runsc",
			Labels:         map[string]string{"ready": "true"},
		},
		Handler: handler,
	}
}
