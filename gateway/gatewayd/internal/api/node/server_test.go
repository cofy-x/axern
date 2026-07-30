package node

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestExecResolvesInjectsLeaseAndForwards(t *testing.T) {
	h := newHarness(t)
	defer h.Close()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-axern-rollout-work-lease", "work-lease"))
	resp, err := h.edge.Exec(ctx, &nodesandboxv1.ExecRequest{
		AllocationID: "alloc-public",
		Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"echo", "ok"}},
	})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if string(resp.GetStdout()) != "ok" {
		t.Fatalf("stdout = %q, want ok", resp.GetStdout())
	}
	if got := h.resolver.requests[0].GetAllocationID(); got != "alloc-public" {
		t.Fatalf("resolved allocation id = %q", got)
	}
	if got := h.resolver.requests[0].GetRolloutExecutionLease(); got != "work-lease" {
		t.Fatalf("rollout execution lease = %q", got)
	}
	if got := h.dialer.targets[0]; got != "node.internal:24010" {
		t.Fatalf("dial target = %q", got)
	}
	req := h.backend.exec
	if req.GetAllocationID() != "alloc-public" || req.GetAttempt() != 7 || req.GetExecutionLeaseToken() != "lease-token" {
		t.Fatalf("backend exec auth fields = allocation %q attempt %d token %q", req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	}
}

func TestMaterializeTaskAssetsResolvesInjectsLeaseAndForwards(t *testing.T) {
	h := newHarness(t)
	defer h.Close()

	_, err := h.edge.MaterializeTaskAssets(context.Background(), &nodesandboxv1.MaterializeTaskAssetsRequest{
		AllocationID: "alloc-public",
		SourcePath:   "tasks/example/verifier/check.sh",
		Target:       "/workspace/.axrun/verifier/check.sh",
		Kind:         nodesandboxv1.TaskAssetKind_TASK_ASSET_KIND_VERIFIER,
	})
	if err != nil {
		t.Fatalf("MaterializeTaskAssets returned error: %v", err)
	}
	req := h.backend.materializeTaskAssets
	if req == nil {
		t.Fatal("backend did not receive materialize request")
	}
	if req.GetAllocationID() != "alloc-public" || req.GetAttempt() != 7 || req.GetExecutionLeaseToken() != "lease-token" {
		t.Fatalf("backend auth fields = allocation %q attempt %d token %q", req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	}
	if req.GetSourcePath() != "tasks/example/verifier/check.sh" || req.GetTarget() != "/workspace/.axrun/verifier/check.sh" {
		t.Fatalf("backend materialize request = %#v", req)
	}
}

func TestProcessBridgesFirstOpenWithInjectedLease(t *testing.T) {
	h := newHarness(t)
	defer h.Close()

	stream, err := h.client.Process(context.Background())
	if err != nil {
		t.Fatalf("Process open returned error: %v", err)
	}
	if err := stream.Send(&nodesandboxv1.ProcessRequest{
		Payload: &nodesandboxv1.ProcessRequest_Open{Open: &nodesandboxv1.ProcessOpen{
			AllocationID: "alloc-public",
			Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"sh", "-c", "echo ok"}},
		}},
	}); err != nil {
		t.Fatalf("Process send open returned error: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("Process CloseSend returned error: %v", err)
	}
	var gotStdout bool
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Process Recv returned error: %v", err)
		}
		if string(resp.GetStdout()) == "ok" {
			gotStdout = true
		}
	}
	if !gotStdout {
		t.Fatal("process stream did not relay stdout")
	}
	open := h.backend.processOpen
	if open.GetAllocationID() != "alloc-public" || open.GetAttempt() != 7 || open.GetExecutionLeaseToken() != "lease-token" {
		t.Fatalf("backend process auth fields = allocation %q attempt %d token %q", open.GetAllocationID(), open.GetAttempt(), open.GetExecutionLeaseToken())
	}
}

func TestProcessRetriesTransientLeaseBeforeReadingClientInput(t *testing.T) {
	h := newHarnessWithOptions(t, Options{
		LeaseRetryAttempts: 2,
		LeaseRetryDelay:    time.Nanosecond,
	})
	defer h.Close()
	h.backend.failProcessReadyOnce = true

	stream, err := h.client.Process(context.Background())
	if err != nil {
		t.Fatalf("Process open returned error: %v", err)
	}
	if err := stream.Send(&nodesandboxv1.ProcessRequest{
		Payload: &nodesandboxv1.ProcessRequest_Open{Open: &nodesandboxv1.ProcessOpen{
			AllocationID: "alloc-public",
			Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"sh", "-c", "echo ok"}},
		}},
	}); err != nil {
		t.Fatalf("Process send open returned error: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("Process CloseSend returned error: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Process initial Recv returned error: %v", err)
	}
	if resp.GetReady() == nil {
		t.Fatalf("initial response = %#v, want ready", resp)
	}
	if got := len(h.resolver.requests); got != 2 {
		t.Fatalf("resolve calls = %d, want 2", got)
	}
	if got := len(h.dialer.targets); got != 2 {
		t.Fatalf("dial calls = %d, want 2", got)
	}
}

func TestExecStreamRefreshesLeaseBeforeBridgingClientInput(t *testing.T) {
	h := newHarnessWithOptions(t, Options{
		LeaseRetryAttempts: 2,
		LeaseRetryDelay:    time.Nanosecond,
	})
	defer h.Close()
	h.resolver.tokens = []string{"stale-token", "fresh-token"}
	h.backend.failExecStreamLeaseOnce = true

	stream, err := h.client.ExecStream(context.Background())
	if err != nil {
		t.Fatalf("ExecStream open returned error: %v", err)
	}
	if err := stream.Send(&nodesandboxv1.ExecStreamRequest{
		Payload: &nodesandboxv1.ExecStreamRequest_Open{Open: &nodesandboxv1.ExecStreamOpen{
			AllocationID: "alloc-public",
			Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"echo", "ok"}},
		}},
	}); err != nil {
		t.Fatalf("ExecStream send open returned error: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("ExecStream CloseSend returned error: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("ExecStream Recv returned error: %v", err)
	}
	if string(resp.GetStdout()) != "ok" {
		t.Fatalf("stdout = %q, want ok", resp.GetStdout())
	}
	if got := len(h.resolver.requests); got != 2 {
		t.Fatalf("resolve calls = %d, want 2", got)
	}
	if got := len(h.backend.execStreamOpens); got != 2 {
		t.Fatalf("backend opens = %d, want 2", got)
	}
	if got := h.backend.execStreamOpens[0].GetExecutionLeaseToken(); got != "stale-token" {
		t.Fatalf("first token = %q, want stale-token", got)
	}
	if got := h.backend.execStreamOpens[1].GetExecutionLeaseToken(); got != "fresh-token" {
		t.Fatalf("second token = %q, want fresh-token", got)
	}
}

func TestProcessEndsWhenUpstreamEndsBeforeClientCloseSend(t *testing.T) {
	h := newHarness(t)
	defer h.Close()

	stream, err := h.client.Process(context.Background())
	if err != nil {
		t.Fatalf("Process open returned error: %v", err)
	}
	if err := stream.Send(&nodesandboxv1.ProcessRequest{
		Payload: &nodesandboxv1.ProcessRequest_Open{Open: &nodesandboxv1.ProcessOpen{
			AllocationID: "alloc-public",
			Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"sh", "-c", "echo ok"}},
		}},
	}); err != nil {
		t.Fatalf("Process send open returned error: %v", err)
	}
	var gotExit bool
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Process Recv returned error: %v", err)
		}
		if resp.GetExit() != nil {
			gotExit = true
		}
	}
	if !gotExit {
		t.Fatal("process stream did not relay exit before EOF")
	}
}

func TestUploadArchiveRequiresOpenFirstMessage(t *testing.T) {
	h := newHarness(t)
	defer h.Close()

	stream, err := h.client.UploadArchive(context.Background())
	if err != nil {
		t.Fatalf("UploadArchive open returned error: %v", err)
	}
	if err := stream.Send(&nodesandboxv1.UploadArchiveRequest{
		Payload: &nodesandboxv1.UploadArchiveRequest_Chunk{Chunk: []byte("not-open")},
	}); err != nil {
		t.Fatalf("UploadArchive send returned error: %v", err)
	}
	_, err = stream.CloseAndRecv()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("UploadArchive CloseAndRecv code = %v, want InvalidArgument (err=%v)", status.Code(err), err)
	}
	if len(h.resolver.requests) != 0 {
		t.Fatalf("resolver called for invalid stream: %d calls", len(h.resolver.requests))
	}
}

func TestUploadArchiveRefreshesLeaseBeforeReadingChunks(t *testing.T) {
	h := newHarnessWithOptions(t, Options{
		LeaseRetryAttempts: 2,
		LeaseRetryDelay:    time.Nanosecond,
	})
	defer h.Close()
	h.resolver.tokens = []string{"stale-token", "fresh-token"}
	h.backend.failUploadArchiveLeaseOnce = true

	stream, err := h.client.UploadArchive(context.Background())
	if err != nil {
		t.Fatalf("UploadArchive open returned error: %v", err)
	}
	if err := stream.Send(&nodesandboxv1.UploadArchiveRequest{
		Payload: &nodesandboxv1.UploadArchiveRequest_Open{Open: &nodesandboxv1.UploadArchiveOpen{
			AllocationID: "alloc-public",
			Path:         "/tmp/archive",
		}},
	}); err != nil {
		t.Fatalf("UploadArchive send open returned error: %v", err)
	}
	if err := stream.Send(&nodesandboxv1.UploadArchiveRequest{
		Payload: &nodesandboxv1.UploadArchiveRequest_Chunk{Chunk: []byte("archive-data")},
	}); err != nil {
		t.Fatalf("UploadArchive send chunk returned error: %v", err)
	}
	if _, err = stream.CloseAndRecv(); err != nil {
		t.Fatalf("UploadArchive CloseAndRecv returned error: %v", err)
	}
	if got := len(h.resolver.requests); got != 2 {
		t.Fatalf("resolve calls = %d, want 2", got)
	}
	if got := len(h.backend.uploadArchiveOpens); got != 2 {
		t.Fatalf("backend opens = %d, want 2", got)
	}
	if got := h.backend.uploadArchiveOpens[0].GetExecutionLeaseToken(); got != "stale-token" {
		t.Fatalf("first token = %q, want stale-token", got)
	}
	if got := h.backend.uploadArchiveOpens[1].GetExecutionLeaseToken(); got != "fresh-token" {
		t.Fatalf("second token = %q, want fresh-token", got)
	}
	if got := string(h.backend.uploadArchiveData); got != "archive-data" {
		t.Fatalf("uploaded data = %q, want archive-data", got)
	}
	if got := h.metrics.routeTypes; len(got) != 1 || got[0] != "node_sandbox" {
		t.Fatalf("lease retry metrics = %#v, want node_sandbox", got)
	}
}

func TestProxyHTTPRefreshesLeaseBeforeReadingBody(t *testing.T) {
	h := newHarnessWithOptions(t, Options{LeaseRetryAttempts: 2, LeaseRetryDelay: time.Nanosecond})
	defer h.Close()
	h.resolver.tokens = []string{"stale-token", "fresh-token"}
	h.backend.failProxyHTTPLeaseOnce = true

	stream, err := h.client.ProxyHTTP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&nodesandboxv1.ProxyHTTPRequest{Payload: &nodesandboxv1.ProxyHTTPRequest_Open{Open: &nodesandboxv1.ProxyHTTPOpen{
		AllocationID: "alloc-public", Port: 8080, Method: "POST", Path: "/upload", HasBody: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&nodesandboxv1.ProxyHTTPRequest{Payload: &nodesandboxv1.ProxyHTTPRequest_Body{Body: []byte("request-body")}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&nodesandboxv1.ProxyHTTPRequest{Payload: &nodesandboxv1.ProxyHTTPRequest_CloseBody{CloseBody: true}}); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	var responseBody []byte
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ProxyHTTP Recv returned error: %v", err)
		}
		responseBody = append(responseBody, resp.GetBody()...)
	}
	if got := string(responseBody); got != "response-body" {
		t.Fatalf("response body = %q, want response-body", got)
	}
	if got := string(h.backend.proxyHTTPData); got != "request-body" {
		t.Fatalf("upstream body = %q, want request-body", got)
	}
	if got := len(h.backend.proxyHTTPOpens); got != 2 {
		t.Fatalf("backend opens = %d, want 2", got)
	}
	if got := len(h.resolver.requests); got != 2 {
		t.Fatalf("resolve calls = %d, want 2", got)
	}
}

func TestDownloadArchiveRefreshesLeaseBeforeSendingBytes(t *testing.T) {
	h := newHarnessWithOptions(t, Options{LeaseRetryAttempts: 2, LeaseRetryDelay: time.Nanosecond})
	defer h.Close()
	h.resolver.tokens = []string{"stale-token", "fresh-token"}
	h.backend.failDownloadArchiveLeaseOnce = true

	stream, err := h.client.DownloadArchive(context.Background(), &nodesandboxv1.DownloadArchiveRequest{
		AllocationID: "alloc-public", Path: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	var archive []byte
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("DownloadArchive Recv returned error: %v", err)
		}
		archive = append(archive, resp.GetChunk()...)
	}
	if got := string(archive); got != "archive-data" {
		t.Fatalf("archive = %q, want archive-data", got)
	}
	if got := len(h.backend.downloadArchiveRequests); got != 2 {
		t.Fatalf("backend requests = %d, want 2", got)
	}
	if got := h.backend.downloadArchiveRequests[0].GetExecutionLeaseToken(); got != "stale-token" {
		t.Fatalf("first token = %q, want stale-token", got)
	}
	if got := h.backend.downloadArchiveRequests[1].GetExecutionLeaseToken(); got != "fresh-token" {
		t.Fatalf("second token = %q, want fresh-token", got)
	}
}

func TestDownloadArchiveDoesNotRetryAfterSendingBytes(t *testing.T) {
	h := newHarnessWithOptions(t, Options{LeaseRetryAttempts: 2, LeaseRetryDelay: time.Nanosecond})
	defer h.Close()
	h.backend.failDownloadArchiveAfterChunk = true

	stream, err := h.client.DownloadArchive(context.Background(), &nodesandboxv1.DownloadArchiveRequest{
		AllocationID: "alloc-public", Path: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := stream.Recv()
	if err != nil || string(resp.GetChunk()) != "archive-data" {
		t.Fatalf("first response = %#v error = %v", resp, err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("final code = %v, want Unauthenticated (err=%v)", status.Code(err), err)
	}
	if got := len(h.resolver.requests); got != 1 {
		t.Fatalf("resolve calls = %d, want 1", got)
	}
}

func TestDownloadArchiveRejectsBackendWithoutLeaseAcknowledgement(t *testing.T) {
	h := newHarnessWithOptions(t, Options{LeaseRetryAttempts: 2, LeaseRetryDelay: time.Nanosecond})
	defer h.Close()
	h.backend.omitDownloadArchiveLeaseHeader = true

	stream, err := h.client.DownloadArchive(context.Background(), &nodesandboxv1.DownloadArchiveRequest{
		AllocationID: "alloc-public", Path: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("response code = %v, want FailedPrecondition (err=%v)", status.Code(err), err)
	}
	if got := len(h.resolver.requests); got != 1 {
		t.Fatalf("resolve calls = %d, want 1", got)
	}
	if got := h.metrics.routeTypes; len(got) != 0 {
		t.Fatalf("lease retry metrics = %#v, want none", got)
	}
}

type harness struct {
	edge     *Server
	client   nodesandboxv1.NodeSandboxClient
	resolver *fakeResolver
	dialer   *fakeDialer
	backend  *fakeBackend
	metrics  *fakeLeaseRetryObserver
	close    func()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithOptions(t, Options{LeaseRetryAttempts: 1})
}

func newHarnessWithOptions(t *testing.T, options Options) *harness {
	t.Helper()
	backend := &fakeBackend{}
	backendServer, backendConn := bufconnServer(t, func(s *grpc.Server) {
		nodesandboxv1.RegisterNodeSandboxServer(s, backend)
	})
	resolver := &fakeResolver{}
	dialer := &fakeDialer{client: nodesandboxv1.NewNodeSandboxClient(backendConn)}
	metrics := &fakeLeaseRetryObserver{}
	options.ClientFingerprint = func(context.Context) (string, error) {
		return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	}
	edge := New(resolver, dialer, options, metrics)
	edgeServer, edgeConn := bufconnServer(t, func(s *grpc.Server) {
		nodesandboxv1.RegisterNodeSandboxServer(s, edge)
	})
	return &harness{
		edge:     edge,
		client:   nodesandboxv1.NewNodeSandboxClient(edgeConn),
		resolver: resolver,
		dialer:   dialer,
		backend:  backend,
		metrics:  metrics,
		close: func() {
			edgeConn.Close()
			backendConn.Close()
			edgeServer.Stop()
			backendServer.Stop()
		},
	}
}

type fakeLeaseRetryObserver struct {
	routeTypes []string
}

func (f *fakeLeaseRetryObserver) LeaseRetry(routeType string) {
	f.routeTypes = append(f.routeTypes, routeType)
}

func (h *harness) Close() {
	h.close()
}

func bufconnServer(t *testing.T, register func(*grpc.Server)) (*grpc.Server, *grpc.ClientConn) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	register(server)
	go func() {
		_ = server.Serve(lis)
	}()
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}), grpc.WithInsecure())
	if err != nil {
		server.Stop()
		t.Fatalf("DialContext returned error: %v", err)
	}
	return server, conn
}

type fakeResolver struct {
	requests []*gatewayv1.ResolveAllocationTerminalRequest
	tokens   []string
}

func (r *fakeResolver) ResolveAllocationTerminal(_ context.Context, req *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	r.requests = append(r.requests, req)
	token := "lease-token"
	if len(r.tokens) > 0 {
		token = r.tokens[0]
		r.tokens = r.tokens[1:]
	}
	return &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: req.GetAllocationID(),
		NodeTarget:   "node.internal:24010",
		Attempt:      7,
		Lease: &commonv1.ExecutionLease{
			PlaintextToken: token,
		},
	}, nil
}

type fakeDialer struct {
	client  nodesandboxv1.NodeSandboxClient
	targets []string
}

func (d *fakeDialer) NodeSandbox(_ context.Context, target string) (nodesandboxv1.NodeSandboxClient, error) {
	d.targets = append(d.targets, target)
	return d.client, nil
}

type fakeBackend struct {
	nodesandboxv1.UnimplementedNodeSandboxServer

	exec                           *nodesandboxv1.ExecRequest
	materializeTaskAssets          *nodesandboxv1.MaterializeTaskAssetsRequest
	processOpen                    *nodesandboxv1.ProcessOpen
	failProcessReadyOnce           bool
	failExecStreamLeaseOnce        bool
	failUploadArchiveLeaseOnce     bool
	failProxyHTTPLeaseOnce         bool
	failDownloadArchiveLeaseOnce   bool
	failDownloadArchiveAfterChunk  bool
	omitDownloadArchiveLeaseHeader bool
	execStreamOpens                []*nodesandboxv1.ExecStreamOpen
	uploadArchiveOpens             []*nodesandboxv1.UploadArchiveOpen
	uploadArchiveData              []byte
	proxyHTTPOpens                 []*nodesandboxv1.ProxyHTTPOpen
	proxyHTTPData                  []byte
	downloadArchiveRequests        []*nodesandboxv1.DownloadArchiveRequest
}

func (b *fakeBackend) Exec(_ context.Context, req *nodesandboxv1.ExecRequest) (*nodesandboxv1.ExecResponse, error) {
	b.exec = req
	return &nodesandboxv1.ExecResponse{ExitCode: 0, Stdout: []byte("ok")}, nil
}

func (b *fakeBackend) MaterializeTaskAssets(_ context.Context, req *nodesandboxv1.MaterializeTaskAssetsRequest) (*nodesandboxv1.MaterializeTaskAssetsResponse, error) {
	b.materializeTaskAssets = req
	return &nodesandboxv1.MaterializeTaskAssetsResponse{}, nil
}

func (b *fakeBackend) ExecStream(stream nodesandboxv1.NodeSandbox_ExecStreamServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	b.execStreamOpens = append(b.execStreamOpens, req.GetOpen())
	if b.failExecStreamLeaseOnce {
		b.failExecStreamLeaseOnce = false
		return status.Error(codes.Unauthenticated, "execution lease is invalid, expired, revoked, or not current")
	}
	if err := stream.SendHeader(metadata.Pairs(nodekernel.ExecutionLeaseAcceptedHeader, "1")); err != nil {
		return err
	}
	if err := stream.Send(&nodesandboxv1.ExecStreamResponse{
		Payload: &nodesandboxv1.ExecStreamResponse_Stdout{Stdout: []byte("ok")},
	}); err != nil {
		return err
	}
	return stream.Send(&nodesandboxv1.ExecStreamResponse{
		Payload: &nodesandboxv1.ExecStreamResponse_Exit{Exit: &nodesandboxv1.ExecExit{ExitCode: 0}},
	})
}

func (b *fakeBackend) Process(stream nodesandboxv1.NodeSandbox_ProcessServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	b.processOpen = req.GetOpen()
	if b.failProcessReadyOnce {
		b.failProcessReadyOnce = false
		return status.Error(codes.Unauthenticated, "execution lease is invalid, expired, revoked, or not current")
	}
	if err := stream.SendHeader(metadata.Pairs(nodekernel.ExecutionLeaseAcceptedHeader, "1")); err != nil {
		return err
	}
	if err := stream.Send(&nodesandboxv1.ProcessResponse{
		Payload: &nodesandboxv1.ProcessResponse_Ready{Ready: &nodesandboxv1.ProcessReady{}},
	}); err != nil {
		return err
	}
	if err := stream.Send(&nodesandboxv1.ProcessResponse{
		Payload: &nodesandboxv1.ProcessResponse_Stdout{Stdout: []byte("ok")},
	}); err != nil {
		return err
	}
	if err := stream.Send(&nodesandboxv1.ProcessResponse{
		Payload: &nodesandboxv1.ProcessResponse_Exit{Exit: &nodesandboxv1.ExecExit{ExitCode: 0}},
	}); err != nil {
		return err
	}
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (b *fakeBackend) UploadArchive(stream nodesandboxv1.NodeSandbox_UploadArchiveServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	b.uploadArchiveOpens = append(b.uploadArchiveOpens, first.GetOpen())
	if b.failUploadArchiveLeaseOnce {
		b.failUploadArchiveLeaseOnce = false
		return status.Error(codes.Unauthenticated, "execution lease is invalid, expired, revoked, or not current")
	}
	if err := stream.SendHeader(metadata.Pairs(nodekernel.ExecutionLeaseAcceptedHeader, "1")); err != nil {
		return err
	}
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		b.uploadArchiveData = append(b.uploadArchiveData, req.GetChunk()...)
	}
	return stream.SendAndClose(&nodesandboxv1.UploadArchiveResponse{})
}

func (b *fakeBackend) ProxyHTTP(stream nodesandboxv1.NodeSandbox_ProxyHTTPServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	b.proxyHTTPOpens = append(b.proxyHTTPOpens, first.GetOpen())
	if b.failProxyHTTPLeaseOnce {
		b.failProxyHTTPLeaseOnce = false
		return status.Error(codes.Unauthenticated, "execution lease is invalid, expired, revoked, or not current")
	}
	if err := stream.SendHeader(metadata.Pairs(nodekernel.ExecutionLeaseAcceptedHeader, "1")); err != nil {
		return err
	}
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if req.GetCloseBody() {
			break
		}
		b.proxyHTTPData = append(b.proxyHTTPData, req.GetBody()...)
	}
	if err := stream.Send(&nodesandboxv1.ProxyHTTPResponse{Payload: &nodesandboxv1.ProxyHTTPResponse_Head{Head: &nodesandboxv1.ProxyHTTPResponseHead{StatusCode: 200}}}); err != nil {
		return err
	}
	return stream.Send(&nodesandboxv1.ProxyHTTPResponse{Payload: &nodesandboxv1.ProxyHTTPResponse_Body{Body: []byte("response-body")}})
}

func (b *fakeBackend) DownloadArchive(req *nodesandboxv1.DownloadArchiveRequest, stream nodesandboxv1.NodeSandbox_DownloadArchiveServer) error {
	b.downloadArchiveRequests = append(b.downloadArchiveRequests, req)
	if b.failDownloadArchiveLeaseOnce {
		b.failDownloadArchiveLeaseOnce = false
		return status.Error(codes.Unauthenticated, "execution lease is invalid, expired, revoked, or not current")
	}
	if !b.omitDownloadArchiveLeaseHeader {
		if err := stream.SendHeader(metadata.Pairs(nodekernel.ExecutionLeaseAcceptedHeader, "1")); err != nil {
			return err
		}
	}
	if err := stream.Send(&nodesandboxv1.DownloadArchiveResponse{Chunk: []byte("archive-data")}); err != nil {
		return err
	}
	if b.failDownloadArchiveAfterChunk {
		return status.Error(codes.Unauthenticated, "post-accept failure")
	}
	return nil
}
