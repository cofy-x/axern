package service

import (
	"context"
	"io"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type execStreamServerStub struct {
	ctx       context.Context
	requests  []*runtime.ExecStreamRequest
	sent      []*runtime.ExecStreamResponse
	recvIndex int
}

func (s *execStreamServerStub) SetHeader(metadata.MD) error  { return nil }
func (s *execStreamServerStub) SendHeader(metadata.MD) error { return nil }
func (s *execStreamServerStub) SetTrailer(metadata.MD)       {}
func (s *execStreamServerStub) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
func (s *execStreamServerStub) SendMsg(any) error { return nil }
func (s *execStreamServerStub) RecvMsg(any) error { return nil }
func (s *execStreamServerStub) Send(resp *runtime.ExecStreamResponse) error {
	s.sent = append(s.sent, resp)
	return nil
}
func (s *execStreamServerStub) Recv() (*runtime.ExecStreamRequest, error) {
	if s.recvIndex >= len(s.requests) {
		return nil, io.EOF
	}
	req := s.requests[s.recvIndex]
	s.recvIndex++
	return req, nil
}

func storeRunningExecContainer(t *testing.T, s *sandboxService, runtimeName string, id string) {
	t.Helper()
	s.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{
		ID:             id,
		RuntimeHandler: runtimeName,
		Labels:         sandboxdReadyTestLabels(),
	})
	time.Sleep(200 * time.Millisecond)
}

func sandboxdReadyTestLabels() map[string]string {
	return map[string]string{
		runtimesandboxd.LabelReady:        "true",
		runtimesandboxd.LabelSocket:       "/tmp/sandboxd.sock",
		runtimesandboxd.LabelCapabilities: "archive,browser,computer_use,desktop,file,probe,process,pty",
	}
}

func storeExitedExecContainer(t *testing.T, s *sandboxService, runtimeName string, id string) {
	t.Helper()
	storeRunningExecContainer(t, s, runtimeName, id)
	assert.NoError(t, s.containerManager.SetExit(id, 0, true, time.Now().UTC(), "", commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED))
}

func TestExecRejectsInvalidArgument(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": &runtimeSpyHandler{name: "runsc", capabilities: contract.RuntimeCapabilities{CanExecDirect: true}},
	})

	_, err := s.Exec(context.Background(), &runtime.ExecRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestExecRejectsExitedContainer(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": &runtimeSpyHandler{name: "runsc", capabilities: contract.RuntimeCapabilities{CanExecDirect: true}},
	})

	storeExitedExecContainer(t, s, "runsc", "axctl-exec-exited")

	_, err := s.Exec(context.Background(), &runtime.ExecRequest{
		ID:      "axctl-exec-exited",
		Command: []string{"/bin/true"},
	})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestExecRequiresRuntimeCapability(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": &runtimeSpyHandler{name: "runsc"},
	})

	storeRunningExecContainer(t, s, "runsc", "axctl-exec-unsupported")

	_, err := s.Exec(context.Background(), &runtime.ExecRequest{
		ID:      "axctl-exec-unsupported",
		Command: []string{"/bin/true"},
	})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestExecReturnsRuntimeExitCodeAndOutput(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		capabilities: contract.RuntimeCapabilities{CanExecDirect: true},
		execResponse: &apipb.ExecContainerResponse{
			ExitCode: 7,
			Stdout:   []byte("ok\n"),
			Stderr:   []byte("warn\n"),
		},
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	storeRunningExecContainer(t, s, "runsc", "axctl-exec-ok")

	resp, err := s.Exec(context.Background(), &runtime.ExecRequest{
		ID:      "axctl-exec-ok",
		Command: []string{"/bin/sh", "-c", "exit 7"},
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(7), resp.GetExitCode())
	assert.Equal(t, []byte("ok\n"), resp.GetStdout())
	assert.Equal(t, []byte("warn\n"), resp.GetStderr())
	assert.Equal(t, "axctl-exec-ok", handler.lastExecOptions.ContainerID)
	assert.Equal(t, "true", handler.lastExecOptions.ContainerLabels[runtimesandboxd.LabelReady])
}

func TestExecStreamRequiresOpenFrame(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": &runtimeSpyHandler{name: "runsc", capabilities: contract.RuntimeCapabilities{CanExecDirect: true}},
	})

	stream := &execStreamServerStub{
		requests: []*runtime.ExecStreamRequest{
			{Payload: &runtime.ExecStreamRequest_CloseStdin{CloseStdin: true}},
		},
	}
	err := s.ExecStream(stream)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestExecStreamForwardsNonTTYStdinAndExit(t *testing.T) {
	session := &execSessionStub{
		chunks: []contract.Chunk{
			{Stdout: []byte("stdout")},
			{Stderr: []byte("stderr")},
		},
		exit: contract.Exit{Status: 3},
	}
	handler := &runtimeSpyHandler{
		name:         "runsc",
		capabilities: contract.RuntimeCapabilities{CanExecDirect: true},
		execSession:  session,
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	storeRunningExecContainer(t, s, "runsc", "axctl-exec-stream")

	stream := &execStreamServerStub{
		requests: []*runtime.ExecStreamRequest{
			{Payload: &runtime.ExecStreamRequest_Open{Open: &runtime.ExecStreamOpen{
				ID:      "axctl-exec-stream",
				Command: []string{"/bin/sh"},
				Tty:     false,
			}}},
			{Payload: &runtime.ExecStreamRequest_Stdin{Stdin: []byte("payload")}},
			{Payload: &runtime.ExecStreamRequest_CloseStdin{CloseStdin: true}},
		},
	}
	assert.NoError(t, s.ExecStream(stream))
	if assert.Len(t, stream.sent, 3) {
		assert.Equal(t, []byte("stdout"), stream.sent[0].GetStdout())
		assert.Equal(t, []byte("stderr"), stream.sent[1].GetStderr())
		assert.Equal(t, int32(3), stream.sent[2].GetExit().GetExitCode())
	}
	assert.Nil(t, handler.lastExecRequest)
	if assert.NotNil(t, handler.lastSessionOpen) {
		assert.False(t, handler.lastSessionOpen.GetTty())
	}
	assert.Equal(t, "axctl-exec-stream", handler.lastSessionOptions.ContainerID)
	assert.Equal(t, "true", handler.lastSessionOptions.ContainerLabels[runtimesandboxd.LabelReady])
	assert.Equal(t, [][]byte{[]byte("payload")}, session.writesSnapshot())
	assert.True(t, session.isStdinClosed())
}

func TestExecStreamForwardsChunksAndExit(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		capabilities: contract.RuntimeCapabilities{CanExecDirect: true},
		execSession: &execSessionStub{
			chunks: []contract.Chunk{{Stdout: []byte("hello")}},
			exit:   contract.Exit{Status: 0},
		},
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	storeRunningExecContainer(t, s, "runsc", "axctl-exec-stream-ok")

	stream := &execStreamServerStub{
		requests: []*runtime.ExecStreamRequest{
			{Payload: &runtime.ExecStreamRequest_Open{Open: &runtime.ExecStreamOpen{
				ID:      "axctl-exec-stream-ok",
				Command: []string{"/bin/sh"},
				Tty:     true,
			}}},
			{Payload: &runtime.ExecStreamRequest_CloseStdin{CloseStdin: true}},
		},
	}

	assert.NoError(t, s.ExecStream(stream))
	if assert.Len(t, stream.sent, 2) {
		assert.Equal(t, []byte("hello"), stream.sent[0].GetStdout())
		assert.Equal(t, int32(0), stream.sent[1].GetExit().GetExitCode())
	}
}

func TestExecStreamPropagatesSessionErrors(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		capabilities: contract.RuntimeCapabilities{CanExecDirect: true},
		execSession: &execSessionStub{
			exit: contract.Exit{},
			err:  errord.ErrUnavailable,
		},
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	storeRunningExecContainer(t, s, "runsc", "axctl-exec-stream-fail")

	stream := &execStreamServerStub{
		requests: []*runtime.ExecStreamRequest{
			{Payload: &runtime.ExecStreamRequest_Open{Open: &runtime.ExecStreamOpen{
				ID:      "axctl-exec-stream-fail",
				Command: []string{"/bin/sh"},
				Tty:     true,
			}}},
		},
	}

	err := s.ExecStream(stream)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}
