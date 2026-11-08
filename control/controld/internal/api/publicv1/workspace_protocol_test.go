package publicv1

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// These servers retain the old create method names and would create disposable
// executions if called. Required Workspace intent must fail before that call.
func TestWorkspaceRunCannotBecomeEphemeralOnOlderControl(t *testing.T) {
	var creates atomic.Int32
	conn := legacyCreateConnection(t, "axern.control.run.v1.RunControl", "CreateRun", func() { creates.Add(1) })
	_, err := runv1.NewRunControlClient(conn).AdmitRun(testProtocolContext(t), &runv1.CreateRunRequest{Namespace: "default", EnvironmentID: "env-example", WorkspaceID: "ws-required"})
	if status.Code(err) != codes.Unimplemented || creates.Load() != 0 {
		t.Fatalf("required Workspace reached old create: code=%v creates=%d", status.Code(err), creates.Load())
	}
}

func TestWorkspaceAllocationCannotBecomeEphemeralOnOlderNode(t *testing.T) {
	var creates atomic.Int32
	conn := legacyCreateConnection(t, "axern.private.node.lifecycle.v1.NodeLifecycle", "CreateAllocation", func() { creates.Add(1) })
	_, err := nodelifecyclev1.NewNodeLifecycleClient(conn).AdmitAllocation(testProtocolContext(t), &nodelifecyclev1.CreateAllocationRequest{AllocationID: "alloc-required", NodeID: "node-example", Config: &nodelifecyclev1.ResolvedExecutionConfig{Workspace: &nodelifecyclev1.WorkspaceBinding{Spec: &nodelifecyclev1.WorkspaceSpec{WorkspaceID: "ws-required"}, Generation: 1, Target: "/workspace"}}})
	if status.Code(err) != codes.Unimplemented || creates.Load() != 0 {
		t.Fatalf("required Workspace reached old create: code=%v creates=%d", status.Code(err), creates.Load())
	}
}

type legacyCreateService interface{}

func legacyCreateConnection(t *testing.T, service, method string, create func()) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	server.RegisterService(&grpc.ServiceDesc{ServiceName: service, HandlerType: (*legacyCreateService)(nil), Methods: []grpc.MethodDesc{{MethodName: method, Handler: func(any, context.Context, func(any) error, grpc.UnaryServerInterceptor) (any, error) {
		create()
		return nil, status.Error(codes.Internal, "old create side effect")
	}}}}, struct{}{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///legacy-workspace-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func testProtocolContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
