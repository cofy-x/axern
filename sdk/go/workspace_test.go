package axernsdk

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type workspaceServer struct {
	workspacev1.UnimplementedWorkspaceControlServer
	*fakeAxernServer
	created                  *workspacev1.CreateWorkspaceRequest
	listed                   *workspacev1.ListWorkspacesRequest
	createCalls, deleteCalls int
	unavailable              bool
	observations             []*workspacev1.Workspace
}

func (f *workspaceServer) value() *workspacev1.Workspace {
	return &workspacev1.Workspace{ID: "ws-retained", NodeID: "node-a", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY, CapacityBytes: 9007199254741504, InodeLimit: 9007199254740995}
}
func (f *workspaceServer) CreateWorkspace(_ context.Context, request *workspacev1.CreateWorkspaceRequest) (*workspacev1.CreateWorkspaceResponse, error) {
	f.created = request
	f.createCalls++
	if f.unavailable {
		return nil, grpcstatus.Error(codes.Unavailable, "inert transport failure")
	}
	return &workspacev1.CreateWorkspaceResponse{Workspace: f.value()}, nil
}
func (f *workspaceServer) GetWorkspace(context.Context, *workspacev1.GetWorkspaceRequest) (*workspacev1.GetWorkspaceResponse, error) {
	value := f.value()
	if len(f.observations) > 0 {
		value = f.observations[0]
		if len(f.observations) > 1 {
			f.observations = f.observations[1:]
		}
	}
	return &workspacev1.GetWorkspaceResponse{Workspace: value}, nil
}
func (f *workspaceServer) ListWorkspaces(_ context.Context, request *workspacev1.ListWorkspacesRequest) (*workspacev1.ListWorkspacesResponse, error) {
	f.listed = request
	return &workspacev1.ListWorkspacesResponse{Workspaces: []*workspacev1.Workspace{f.value()}, NextCursor: "next"}, nil
}
func (f *workspaceServer) DeleteWorkspace(context.Context, *workspacev1.DeleteWorkspaceRequest) (*workspacev1.DeleteWorkspaceResponse, error) {
	f.deleteCalls++
	if f.unavailable {
		return nil, grpcstatus.Error(codes.Unavailable, "inert transport failure")
	}
	return &workspacev1.DeleteWorkspaceResponse{Workspace: &workspacev1.Workspace{ID: "ws-retained", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETING}}, nil
}

func workspaceClient(t *testing.T, register func(*grpc.Server)) *Client {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	register(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := NewClient(ctx, "bufnet", WithDialOptions(grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }), grpc.WithTransportCredentials(insecure.NewCredentials())))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestWorkspaceCRUDTransportKeepsInt64AndSandboxCloseRetainsData(t *testing.T) {
	fake := &workspaceServer{fakeAxernServer: &fakeAxernServer{}}
	client := workspaceClient(t, func(server *grpc.Server) {
		workspacev1.RegisterWorkspaceControlServer(server, fake)
		runv1.RegisterRunControlServer(server, fake.fakeAxernServer)
		environmentv1.RegisterEnvironmentControlServer(server, fake.fakeAxernServer)
	})
	value, err := client.CreateWorkspace(t.Context(), CreateWorkspaceOptions{NodeID: "node-a", CapacityBytes: 9007199254741504, InodeLimit: 9007199254740995, Labels: map[string]string{"project": "inert"}})
	if err != nil {
		t.Fatal(err)
	}
	if fake.created.GetCapacityBytes() != 9007199254741504 || value.GetInodeLimit() != 9007199254740995 || fake.created.GetNamespace() != "default" {
		t.Fatalf("capacity precision changed: %v %v", fake.created, value)
	}
	if value, err = client.GetWorkspace(t.Context(), "ws-retained"); err != nil || value.GetID() != "ws-retained" {
		t.Fatalf("get: %v %v", value, err)
	}
	listed, err := client.ListWorkspaces(t.Context(), ListWorkspacesOptions{Namespace: "team", Cursor: "current", PageSize: 7})
	if err != nil || listed.GetNextCursor() != "next" || fake.listed.GetNamespace() != "team" || fake.listed.GetCursor() != "current" {
		t.Fatalf("list: %v %v", listed, err)
	}
	sandbox, err := NewSandbox(SandboxOptions{Client: client, EnvironmentID: "env-existing", WorkspaceID: "ws-retained"})
	if err != nil {
		t.Fatal(err)
	}
	if err = sandbox.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fake.createRunRequest.GetWorkspaceID() != "ws-retained" {
		t.Fatal("required Workspace intent lost")
	}
	if err = sandbox.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !fake.cancelledRun || fake.deleteCalls != 0 {
		t.Fatal("closing Sandbox deleted retained data or skipped execution cleanup")
	}
	deleted, err := client.DeleteWorkspace(t.Context(), "ws-retained")
	if err != nil || deleted.GetStatus() != workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETING || fake.deleteCalls != 1 {
		t.Fatalf("explicit deletion: %v %v", deleted, err)
	}
	fake.unavailable = true
	_, err = client.CreateWorkspace(t.Context(), CreateWorkspaceOptions{NodeID: "node-a", CapacityBytes: 4096, InodeLimit: 64})
	if err == nil || fake.createCalls != 2 {
		t.Fatalf("create mutation retried implicitly: %d %v", fake.createCalls, err)
	}
	_, err = client.DeleteWorkspace(t.Context(), "ws-retained")
	if err == nil || fake.deleteCalls != 2 {
		t.Fatalf("delete mutation retried implicitly: %d %v", fake.deleteCalls, err)
	}
}

type legacyRunService interface {
	CreateRun(context.Context, *runv1.CreateRunRequest) (*runv1.CreateRunResponse, error)
}
type legacyRunServer struct{ calls int }

func (f *legacyRunServer) CreateRun(context.Context, *runv1.CreateRunRequest) (*runv1.CreateRunResponse, error) {
	f.calls++
	return &runv1.CreateRunResponse{Run: &runv1.Run{ID: "unexpected-ephemeral"}}, nil
}

func TestRequiredWorkspaceCannotFallBackToOldCreateRunWire(t *testing.T) {
	legacy := &legacyRunServer{}
	client := workspaceClient(t, func(server *grpc.Server) {
		server.RegisterService(&grpc.ServiceDesc{
			ServiceName: "axern.control.run.v1.RunControl", HandlerType: (*legacyRunService)(nil),
			Methods: []grpc.MethodDesc{{MethodName: "CreateRun", Handler: func(srv any, ctx context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				req := &runv1.CreateRunRequest{}
				if err := decode(req); err != nil {
					return nil, err
				}
				return srv.(legacyRunService).CreateRun(ctx, req)
			}}},
		}, legacy)
	})
	_, err := client.CreateRun(t.Context(), CreateRunOptions{EnvironmentID: "env-existing", WorkspaceID: "ws-retained"})
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != codes.Unimplemented || legacy.calls != 0 {
		t.Fatalf("old wire accepted required attachment: calls=%d err=%v", legacy.calls, err)
	}
}

func TestWorkspaceReadyWaitsForWriterCleanupWithoutAcquiringAuthority(t *testing.T) {
	fake := &workspaceServer{fakeAxernServer: &fakeAxernServer{}, observations: []*workspacev1.Workspace{
		{ID: "ws-retained", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY, AttachedAllocationID: "alloc-old"},
		{ID: "ws-retained", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY},
	}}
	client := workspaceClient(t, func(server *grpc.Server) { workspacev1.RegisterWorkspaceControlServer(server, fake) })
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	value, err := client.WaitWorkspaceReady(ctx, "ws-retained")
	if err != nil || value.GetAttachedAllocationID() != "" {
		t.Fatalf("writer cleanup observation: %v %v", value, err)
	}
	if len(fake.observations) != 1 || fake.createCalls != 0 || fake.deleteCalls != 0 {
		t.Fatal("waiter acquired or deleted data instead of observing cleanup")
	}
	fake.observations = []*workspacev1.Workspace{{ID: "different", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY}}
	if _, err = client.WaitWorkspaceReady(ctx, "ws-retained"); err == nil || !strings.Contains(err.Error(), "different identity") {
		t.Fatalf("wrong readiness identity accepted: %v", err)
	}
}

func TestUnalignedWorkspaceCapacityRejectedBeforeMutation(t *testing.T) {
	client := &Client{}
	for _, capacity := range []int64{0, -512, 513} {
		if _, err := client.CreateWorkspace(t.Context(), CreateWorkspaceOptions{NodeID: "node-a", CapacityBytes: capacity, InodeLimit: 64}); err == nil {
			t.Fatalf("invalid capacity reached transport: %d", capacity)
		}
	}
}
