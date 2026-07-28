package service

import (
	"context"
	"io"
	"testing"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type processStreamServerStub struct {
	ctx       context.Context
	requests  []*runtime.ProcessRequest
	sent      []*runtime.ProcessResponse
	recvIndex int
	recvEOF   func()
}

func (s *processStreamServerStub) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *processStreamServerStub) Send(resp *runtime.ProcessResponse) error {
	s.sent = append(s.sent, resp)
	return nil
}

func (s *processStreamServerStub) Recv() (*runtime.ProcessRequest, error) {
	if s.recvIndex >= len(s.requests) {
		if s.recvEOF != nil {
			s.recvEOF()
			s.recvEOF = nil
		}
		return nil, io.EOF
	}
	req := s.requests[s.recvIndex]
	s.recvIndex++
	return req, nil
}

func TestProcessForwardsStdinSignalAndExit(t *testing.T) {
	inputDone := make(chan struct{})
	session := &execSessionStub{
		chunks:      []contract.Chunk{{Stdout: []byte("ok\n")}},
		exit:        contract.Exit{Timestamp: time.Now(), Status: 0},
		recvEOFWait: inputDone,
	}
	handler := &runtimeSpyHandler{
		name:         "runsc",
		capabilities: contract.RuntimeCapabilities{CanExecDirect: true},
		execSession:  session,
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	storeRunningExecContainer(t, s, "runsc", "axctl-process")

	stream := &processStreamServerStub{recvEOF: func() { close(inputDone) }, requests: []*runtime.ProcessRequest{
		{Payload: &runtime.ProcessRequest_Open{Open: &runtime.ProcessOpen{
			ID:      "axctl-process",
			Command: []string{"/bin/cat"},
		}}},
		{Payload: &runtime.ProcessRequest_Stdin{Stdin: []byte("payload")}},
		{Payload: &runtime.ProcessRequest_Resize{Resize: &runtime.TerminalResize{Cols: 120, Rows: 40}}},
		{Payload: &runtime.ProcessRequest_Signal{Signal: &runtime.ProcessSignal{Signal: "TERM"}}},
		{Payload: &runtime.ProcessRequest_CloseStdin{CloseStdin: true}},
	}}

	require.NoError(t, s.Process(stream))
	assert.Equal(t, []byte("payload"), session.writes[0])
	assert.Equal(t, [][2]uint32{{120, 40}}, session.resizeCalls)
	assert.Equal(t, []string{"TERM"}, session.signals)
	assert.True(t, session.stdinClosed)
	require.Len(t, stream.sent, 3)
	assert.NotNil(t, stream.sent[0].GetReady())
	assert.Equal(t, "ok\n", string(stream.sent[1].GetStdout()))
	assert.Equal(t, int32(0), stream.sent[2].GetExit().GetExitCode())
}

func TestProcessSendsOutputBeforeExit(t *testing.T) {
	session := &execSessionStub{
		chunks: []contract.Chunk{
			{Stdout: []byte("stdout-before-exit\n")},
			{Stderr: []byte("stderr-before-exit\n")},
		},
		exit: contract.Exit{Timestamp: time.Now(), Status: 7},
	}
	handler := &runtimeSpyHandler{
		name:         "runsc",
		capabilities: contract.RuntimeCapabilities{CanExecDirect: true},
		execSession:  session,
	}
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	storeRunningExecContainer(t, s, "runsc", "axctl-process-order")

	stream := &processStreamServerStub{requests: []*runtime.ProcessRequest{
		{Payload: &runtime.ProcessRequest_Open{Open: &runtime.ProcessOpen{
			ID:      "axctl-process-order",
			Command: []string{"/bin/sh", "-lc", "printf stdout-before-exit; printf stderr-before-exit >&2; exit 7"},
		}}},
	}}

	require.NoError(t, s.Process(stream))
	require.Len(t, stream.sent, 4)
	assert.NotNil(t, stream.sent[0].GetReady())
	assert.Equal(t, "stdout-before-exit\n", string(stream.sent[1].GetStdout()))
	assert.Equal(t, "stderr-before-exit\n", string(stream.sent[2].GetStderr()))
	assert.Equal(t, int32(7), stream.sent[3].GetExit().GetExitCode())
}
