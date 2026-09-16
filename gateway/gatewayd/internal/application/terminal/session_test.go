package terminal

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (f *fakeTerminalResolver) AuthorizeAllocationAccess(context.Context, *gatewayv1.ResolveAllocationTerminalRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, f.authorizeErr
}

func TestOpenResolvedRetriesSameGrantBeforeReturningSession(t *testing.T) {
	t.Parallel()
	stale := &fakeProcessStream{headerErr: status.Error(codes.Unauthenticated, "stale lease")}
	fresh := &fakeProcessStream{responses: []*nodesandboxv1.ProcessResponse{{
		Payload: &nodesandboxv1.ProcessResponse_Ready{Ready: &nodesandboxv1.ProcessReady{}},
	}}}
	nodes := &fakeProcessStreamer{streams: []*fakeProcessStream{stale, fresh}}
	resolver := &fakeTerminalResolver{}
	manager := NewManager(resolver, nodes, Options{AccessGrantRetryAttempts: 2, AccessGrantRetryDelay: time.Nanosecond}, nil, nil)

	session, err := manager.OpenResolved(WithCredential(context.Background(), "test-fingerprint", "x509_sha256"), &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		NodeID:       "node-old",
		NodeTarget:   "node-old:24010",
		AccessGrant:  &gatewayv1.AllocationAccessGrant{ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), PlaintextToken: "pending-token"},
	})
	if err != nil {
		t.Fatalf("OpenResolved() error = %v", err)
	}
	defer session.Close()
	if got := nodes.targets; len(got) != 2 || got[0] != "node-old:24010" || got[1] != "node-old:24010" {
		t.Fatalf("node targets = %#v, want the immutable binding twice", got)
	}
	if len(resolver.requests) != 0 {
		t.Fatalf("resolve requests = %#v, want no replacement grant", resolver.requests)
	}
	if got := nodes.tokens[0]; got != "pending-token" {
		t.Fatalf("initial access grant token = %q", got)
	}
	if got := nodes.tokens[1]; got != "pending-token" {
		t.Fatalf("retried access grant token = %q", got)
	}
	if stale.closeCalls != 1 {
		t.Fatalf("stale stream close calls = %d, want 1", stale.closeCalls)
	}
}

func TestOpenResolvedLeaseBackoffHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(WithCredential(context.Background(), "test-fingerprint", "x509_sha256"))
	cancel()
	nodes := &fakeProcessStreamer{streams: []*fakeProcessStream{{headerErr: status.Error(codes.Unauthenticated, "stale lease")}}}
	resolver := &fakeTerminalResolver{}
	manager := NewManager(resolver, nodes, Options{AccessGrantRetryAttempts: 2, AccessGrantRetryDelay: time.Hour}, nil, nil)

	_, err := manager.OpenResolved(ctx, &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		NodeTarget:   "node-old:24010",
		AccessGrant:  &gatewayv1.AllocationAccessGrant{ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), PlaintextToken: "stale-token"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenResolved() error = %v, want context.Canceled", err)
	}
	if len(resolver.requests) != 0 {
		t.Fatalf("resolve calls = %d, want 0 after cancellation", len(resolver.requests))
	}
}

func TestProcessOpenRequestUsesShellTTYAndAllocation(t *testing.T) {
	t.Parallel()
	req := processOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		AccessGrant: &gatewayv1.AllocationAccessGrant{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{})
	open := req.GetOpen()
	if open.GetAllocationID() != "alloc-1" {
		t.Fatalf("open allocation = %#v", open)
	}
	if got := open.GetSpec().GetArgv(); len(got) != 1 || got[0] != "/bin/sh" || !open.GetSpec().GetTty() {
		t.Fatalf("open spec = %#v", open.GetSpec())
	}
}

func TestProcessOpenRequestUsesCustomArgv(t *testing.T) {
	t.Parallel()
	req := processOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		AccessGrant: &gatewayv1.AllocationAccessGrant{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{Argv: []string{"/bin/bash", "-l"}})
	got := req.GetOpen().GetSpec().GetArgv()
	if len(got) != 2 || got[0] != "/bin/bash" || got[1] != "-l" || req.GetOpen().GetSpec().GetTty() {
		t.Fatalf("open spec = %#v", req.GetOpen().GetSpec())
	}
}

func TestProcessOpenRequestUsesCustomArgvTTY(t *testing.T) {
	t.Parallel()
	req := processOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		AccessGrant: &gatewayv1.AllocationAccessGrant{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{Argv: []string{"/bin/bash"}, TTY: true})
	if !req.GetOpen().GetSpec().GetTty() {
		t.Fatalf("open spec = %#v", req.GetOpen().GetSpec())
	}
}

func TestProcessOpenRequestCarriesInitialTerminalSize(t *testing.T) {
	t.Parallel()
	req := processOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{AllocationID: "alloc-1"}, OpenOptions{
		TTY: true, InitialCols: 120, InitialRows: 40,
	})
	if size := req.GetOpen().GetInitialSize(); size.GetCols() != 120 || size.GetRows() != 40 {
		t.Fatalf("initial terminal size = %#v", size)
	}
}

func TestProcessOpenRequestUsesEnv(t *testing.T) {
	t.Parallel()
	req := processOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		AccessGrant: &gatewayv1.AllocationAccessGrant{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{Env: map[string]string{"TERM": "xterm-256color"}})
	got := req.GetOpen().GetSpec().GetEnv()
	if got["TERM"] != "xterm-256color" {
		t.Fatalf("open env = %#v", got)
	}
}

func TestProcessOpenRequestUsesUser(t *testing.T) {
	t.Parallel()
	req := processOpenRequest(&gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		AccessGrant: &gatewayv1.AllocationAccessGrant{
			PlaintextToken: "lease-token",
		},
	}, OpenOptions{User: " axern "})
	if got := req.GetOpen().GetSpec().GetUser(); got != "axern" {
		t.Fatalf("open user = %q, want axern", got)
	}
}

func TestSessionWriteResizeAndRecv(t *testing.T) {
	t.Parallel()
	stream := &fakeProcessStream{
		responses: []*nodesandboxv1.ProcessResponse{
			{Payload: &nodesandboxv1.ProcessResponse_Stdout{Stdout: []byte("out")}},
			{Payload: &nodesandboxv1.ProcessResponse_Stderr{Stderr: []byte("err")}},
			{Payload: &nodesandboxv1.ProcessResponse_Exit{Exit: &nodesandboxv1.ExecExit{ExitCode: 7, Message: "done"}}},
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

type fakeProcessStream struct {
	sent       []*nodesandboxv1.ProcessRequest
	responses  []*nodesandboxv1.ProcessResponse
	headerErr  error
	closeCalls int
}

func (f *fakeProcessStream) Send(req *nodesandboxv1.ProcessRequest) error {
	f.sent = append(f.sent, req)
	return nil
}

func (f *fakeProcessStream) Recv() (*nodesandboxv1.ProcessResponse, error) {
	if len(f.responses) == 0 {
		return nil, io.EOF
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeProcessStream) Header() (metadata.MD, error) {
	if f.headerErr != nil {
		return nil, f.headerErr
	}
	return metadata.Pairs(nodekernel.AllocationAccessGrantAcceptedHeader, "1"), nil
}
func (f *fakeProcessStream) Trailer() metadata.MD { return nil }
func (f *fakeProcessStream) CloseSend() error {
	f.closeCalls++
	return nil
}
func (f *fakeProcessStream) Context() context.Context {
	return WithCredential(context.Background(), "test-fingerprint", "x509_sha256")
}
func (f *fakeProcessStream) SendMsg(any) error { return nil }
func (f *fakeProcessStream) RecvMsg(any) error { return nil }

type fakeProcessStreamer struct {
	ctx     context.Context
	streams []*fakeProcessStream
	targets []string
	tokens  []string
}

func (f *fakeProcessStreamer) Process(ctx context.Context, target, nodeID string) (nodesandboxv1.NodeSandbox_ProcessClient, error) {
	f.ctx = ctx
	f.targets = append(f.targets, target)
	md, _ := metadata.FromOutgoingContext(ctx)
	values := md.Get(nodekernel.AllocationAccessGrantTokenMetadata)
	if len(values) == 1 {
		f.tokens = append(f.tokens, values[0])
	} else {
		f.tokens = append(f.tokens, "")
	}
	if len(f.streams) == 0 {
		return nil, errors.New("unexpected process stream")
	}
	stream := f.streams[0]
	f.streams = f.streams[1:]
	return stream, nil
}

type fakeTerminalResolver struct {
	authorizeErr error
	responses    []*gatewayv1.ResolveAllocationTerminalResponse
	requests     []*gatewayv1.ResolveAllocationTerminalRequest
}

func TestSessionAuthorityLossCancelsUpstream(t *testing.T) {
	for _, reason := range []error{status.Error(codes.PermissionDenied, "revoked"), status.Error(codes.Unavailable, "control unavailable")} {
		t.Run(reason.Error(), func(t *testing.T) {
			t.Parallel()
			nodes := &fakeProcessStreamer{streams: []*fakeProcessStream{{responses: []*nodesandboxv1.ProcessResponse{{Payload: &nodesandboxv1.ProcessResponse_Ready{Ready: &nodesandboxv1.ProcessReady{}}}}}}}
			manager := NewManager(&fakeTerminalResolver{authorizeErr: reason}, nodes, Options{}, nil, nil)
			session, err := manager.OpenResolved(WithCredential(context.Background(), "fingerprint", "ssh_sha256"), &gatewayv1.ResolveAllocationTerminalResponse{AllocationID: "allocation", AccessGrant: &gatewayv1.AllocationAccessGrant{ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			select {
			case <-nodes.ctx.Done():
			case <-time.After(20 * time.Second):
				t.Fatal("authorization loss did not cancel process stream")
			}
		})
	}
}

func TestSessionRejectsExpiredAuthority(t *testing.T) {
	manager := NewManager(nil, nil, Options{}, nil, nil)
	_, err := manager.OpenResolved(context.Background(), &gatewayv1.ResolveAllocationTerminalResponse{AccessGrant: &gatewayv1.AllocationAccessGrant{ExpiresAt: timestamppb.New(time.Now().Add(-time.Second))}})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expired grant accepted: %v", err)
	}
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
