package terminal

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestOpenResolvedRefreshesRejectedLeaseBeforeReturningSession(t *testing.T) {
	t.Parallel()
	stale := &fakeExecStream{headerErr: status.Error(codes.Unauthenticated, "stale lease")}
	fresh := &fakeExecStream{}
	nodes := &fakeExecStreamer{streams: []*fakeExecStream{stale, fresh}}
	resolver := &fakeTerminalResolver{responses: []*gatewayv1.ResolveAllocationTerminalResponse{{
		AllocationID: "alloc-1",
		NodeID:       "node-new",
		NodeTarget:   "node-new:24010",
		Attempt:      2,
		Lease:        &commonv1.ExecutionLease{PlaintextToken: "fresh-token"},
	}}}
	manager := NewManager(resolver, nodes, Options{LeaseRetryAttempts: 2, LeaseRetryDelay: time.Nanosecond}, nil, nil)

	session, err := manager.OpenResolved(context.Background(), &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		NodeID:       "node-old",
		NodeTarget:   "node-old:24010",
		Attempt:      1,
		Lease:        &commonv1.ExecutionLease{PlaintextToken: "stale-token"},
	})
	if err != nil {
		t.Fatalf("OpenResolved() error = %v", err)
	}
	defer session.Close()
	if got := nodes.targets; len(got) != 2 || got[0] != "node-old:24010" || got[1] != "node-new:24010" {
		t.Fatalf("node targets = %#v, want old then new", got)
	}
	if len(resolver.requests) != 1 || resolver.requests[0].GetAllocationID() != "alloc-1" {
		t.Fatalf("resolve requests = %#v, want one alloc-1 refresh", resolver.requests)
	}
	if got := stale.sent[0].GetOpen().GetExecutionLeaseToken(); got != "stale-token" {
		t.Fatalf("stale attempt token = %q", got)
	}
	if got := fresh.sent[0].GetOpen().GetExecutionLeaseToken(); got != "fresh-token" {
		t.Fatalf("fresh attempt token = %q", got)
	}
	if stale.closeCalls != 1 {
		t.Fatalf("stale stream close calls = %d, want 1", stale.closeCalls)
	}
}

func TestOpenResolvedLeaseBackoffHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	nodes := &fakeExecStreamer{streams: []*fakeExecStream{{headerErr: status.Error(codes.Unauthenticated, "stale lease")}}}
	resolver := &fakeTerminalResolver{}
	manager := NewManager(resolver, nodes, Options{LeaseRetryAttempts: 2, LeaseRetryDelay: time.Hour}, nil, nil)

	_, err := manager.OpenResolved(ctx, &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		NodeTarget:   "node-old:24010",
		Attempt:      1,
		Lease:        &commonv1.ExecutionLease{PlaintextToken: "stale-token"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenResolved() error = %v, want context.Canceled", err)
	}
	if len(resolver.requests) != 0 {
		t.Fatalf("resolve calls = %d, want 0 after cancellation", len(resolver.requests))
	}
}

func TestExecStreamOpenRequestUsesShellTTYAndLease(t *testing.T) {
	t.Parallel()
	req := execStreamOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		Attempt:      2,
		Lease: &commonv1.ExecutionLease{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{})
	open := req.GetOpen()
	if open.GetAllocationID() != "alloc-1" || open.GetAttempt() != 2 || open.GetExecutionLeaseToken() != "lease-token" {
		t.Fatalf("open auth = %#v", open)
	}
	if got := open.GetSpec().GetArgv(); len(got) != 1 || got[0] != "/bin/sh" || !open.GetSpec().GetTty() {
		t.Fatalf("open spec = %#v", open.GetSpec())
	}
}

func TestExecStreamOpenRequestUsesCustomArgv(t *testing.T) {
	t.Parallel()
	req := execStreamOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		Attempt:      2,
		Lease: &commonv1.ExecutionLease{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{Argv: []string{"/bin/bash", "-l"}})
	got := req.GetOpen().GetSpec().GetArgv()
	if len(got) != 2 || got[0] != "/bin/bash" || got[1] != "-l" || req.GetOpen().GetSpec().GetTty() {
		t.Fatalf("open spec = %#v", req.GetOpen().GetSpec())
	}
}

func TestExecStreamOpenRequestUsesCustomArgvTTY(t *testing.T) {
	t.Parallel()
	req := execStreamOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		Attempt:      2,
		Lease: &commonv1.ExecutionLease{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{Argv: []string{"/bin/bash"}, TTY: true})
	if !req.GetOpen().GetSpec().GetTty() {
		t.Fatalf("open spec = %#v", req.GetOpen().GetSpec())
	}
}

func TestExecStreamOpenRequestUsesEnv(t *testing.T) {
	t.Parallel()
	req := execStreamOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		Attempt:      2,
		Lease: &commonv1.ExecutionLease{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{Env: map[string]string{"TERM": "xterm-256color"}})
	got := req.GetOpen().GetSpec().GetEnv()
	if got["TERM"] != "xterm-256color" {
		t.Fatalf("open env = %#v", got)
	}
}

func TestExecStreamOpenRequestUsesUser(t *testing.T) {
	t.Parallel()
	req := execStreamOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		Attempt:      2,
		Lease: &commonv1.ExecutionLease{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{User: " axern "})
	if got := req.GetOpen().GetSpec().GetUser(); got != "axern" {
		t.Fatalf("open user = %q, want axern", got)
	}
}

func TestSessionWriteResizeAndRecv(t *testing.T) {
	t.Parallel()
	stream := &fakeExecStream{
		responses: []*nodesandboxv1.ExecStreamResponse{
			{Payload: &nodesandboxv1.ExecStreamResponse_Stdout{Stdout: []byte("out")}},
			{Payload: &nodesandboxv1.ExecStreamResponse_Stderr{Stderr: []byte("err")}},
			{Payload: &nodesandboxv1.ExecStreamResponse_Exit{Exit: &nodesandboxv1.ExecExit{ExitCode: 7, Message: "done"}}},
		},
	}
	session := &Session{stream: stream}
	if err := session.Write([]byte("input")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := session.Resize(120, 40); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	if err := session.CloseStdin(); err != nil {
		t.Fatalf("CloseStdin() error = %v", err)
	}
	if got := stream.sent[0].GetStdin(); string(got) != "input" {
		t.Fatalf("stdin = %q, want input", string(got))
	}
	if resize := stream.sent[1].GetResize(); resize.GetCols() != 120 || resize.GetRows() != 40 {
		t.Fatalf("resize = %#v", resize)
	}
	if !stream.sent[2].GetCloseStdin() {
		t.Fatal("close_stdin = false, want true")
	}

	out, err := session.Recv()
	if err != nil || string(out.Stdout) != "out" {
		t.Fatalf("Recv stdout = %#v err=%v", out, err)
	}
	out, err = session.Recv()
	if err != nil || string(out.Stderr) != "err" {
		t.Fatalf("Recv stderr = %#v err=%v", out, err)
	}
	out, err = session.Recv()
	if err != nil || out.Exit == nil || out.Exit.Code != 7 || out.Exit.Message != "done" {
		t.Fatalf("Recv exit = %#v err=%v", out, err)
	}
}

type fakeExecStream struct {
	sent       []*nodesandboxv1.ExecStreamRequest
	responses  []*nodesandboxv1.ExecStreamResponse
	headerErr  error
	closeCalls int
}

func (f *fakeExecStream) Send(req *nodesandboxv1.ExecStreamRequest) error {
	f.sent = append(f.sent, req)
	return nil
}

func (f *fakeExecStream) Recv() (*nodesandboxv1.ExecStreamResponse, error) {
	if len(f.responses) == 0 {
		return nil, io.EOF
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeExecStream) Header() (metadata.MD, error) {
	if f.headerErr != nil {
		return nil, f.headerErr
	}
	return metadata.Pairs(nodekernel.ExecutionLeaseAcceptedHeader, "1"), nil
}
func (f *fakeExecStream) Trailer() metadata.MD { return nil }
func (f *fakeExecStream) CloseSend() error {
	f.closeCalls++
	return nil
}
func (f *fakeExecStream) Context() context.Context { return context.Background() }
func (f *fakeExecStream) SendMsg(any) error        { return nil }
func (f *fakeExecStream) RecvMsg(any) error        { return nil }

type fakeExecStreamer struct {
	streams []*fakeExecStream
	targets []string
}

func (f *fakeExecStreamer) ExecStream(_ context.Context, target string) (nodesandboxv1.NodeSandbox_ExecStreamClient, error) {
	f.targets = append(f.targets, target)
	if len(f.streams) == 0 {
		return nil, errors.New("unexpected exec stream")
	}
	stream := f.streams[0]
	f.streams = f.streams[1:]
	return stream, nil
}

type fakeTerminalResolver struct {
	responses []*gatewayv1.ResolveAllocationTerminalResponse
	requests  []*gatewayv1.ResolveAllocationTerminalRequest
}

func (f *fakeTerminalResolver) ResolveAllocationTerminal(_ context.Context, req *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, errors.New("unexpected resolve")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}
