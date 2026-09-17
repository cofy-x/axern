package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	gatewayv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	"io"
	"testing"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	controlplane "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeNodeSandboxService struct {
	execRequests            []*runtimev1.ExecRequest
	statFileRequests        []*runtimev1.StatFileRequest
	listDirRequests         []*runtimev1.ListDirRequest
	readFileRequests        []*runtimev1.ReadFileRequest
	writeFileRequests       []*runtimev1.WriteFileRequest
	mkdirRequests           []*runtimev1.MkdirRequest
	removeRequests          []*runtimev1.RemoveRequest
	existsRequests          []*runtimev1.ExistsRequest
	copyRequests            []*runtimev1.CopyRequest
	moveRequests            []*runtimev1.MoveRequest
	chmodRequests           []*runtimev1.ChmodRequest
	touchRequests           []*runtimev1.TouchRequest
	uploadArchiveRequests   []*runtimev1.UploadArchiveRequest
	downloadArchiveRequests []*runtimev1.DownloadArchiveRequest
	computerUseStatusReqs   []*runtimev1.ComputerUseStatusRequest
	computerUseScreenReqs   []*runtimev1.ComputerUseScreenshotRequest
	computerUseDisplayReqs  []*runtimev1.ComputerUseDisplayRequest
	computerUseMouseReqs    []*runtimev1.ComputerUseMouseRequest
	computerUseKeyboardReqs []*runtimev1.ComputerUseKeyboardRequest
	capabilityStatusIDs     []string
	waitRequests            []*runtimev1.WaitRequest
	reportedAllocationID    string
	reportedStatus          commonv1.AllocationLifecycleState
	reportedExitCode        int32
	reportedKnown           bool
	reportedMessage         string
	processFunc             func(service.ProcessStreamServer) error
	controlPlaneIDs         map[string]bool
}

func (f *fakeNodeSandboxService) ReadAllocationOutput(ctx context.Context, id, cursor string) ([]allocationoutput.Chunk, bool, error) {
	return allocationoutput.New(f).Read(ctx, id, cursor)
}

func (f *fakeNodeSandboxService) SealedOutputManifest(context.Context, string) (allocationoutput.Manifest, error) {
	return allocationoutput.Manifest{}, nil
}

func (f *fakeNodeSandboxService) ReadSealedOutput(context.Context, string, string, int64, int64) ([]byte, int64, bool, error) {
	return nil, 0, true, nil
}
func allocationAccessIncomingContext(parent context.Context, token string) context.Context {
	return metadata.NewIncomingContext(parent, metadata.Pairs(accessGrantTokenMetadataKey, token))
}

func (f *fakeNodeSandboxService) Run(context.Context) error      { return nil }
func (f *fakeNodeSandboxService) Shutdown(context.Context) error { return nil }
func (f *fakeNodeSandboxService) Start(context.Context, *runtimev1.StartRequest) (*runtimev1.StartResponse, error) {
	return nil, nil
}
func (f *fakeNodeSandboxService) Delete(context.Context, *runtimev1.DeleteRequest) (*runtimev1.DeleteResponse, error) {
	return nil, nil
}
func (f *fakeNodeSandboxService) ExecStream(stream service.ExecStreamServer) error {
	_ = stream
	return nil
}
func (f *fakeNodeSandboxService) Process(stream service.ProcessStreamServer) error {
	if f.processFunc != nil {
		return f.processFunc(stream)
	}
	return nil
}
func (f *fakeNodeSandboxService) Ready() bool { return true }
func (f *fakeNodeSandboxService) IsControlPlaneAllocation(allocationID string) bool {
	return f.controlPlaneIDs[allocationID]
}
func (f *fakeNodeSandboxService) NodeInventory() (nodeinventory.NodeInventorySnapshot, bool) {
	return nodeinventory.NewSnapshot(), false
}
func (f *fakeNodeSandboxService) List(context.Context, *runtimev1.ListContainersRequest) (*runtimev1.ListContainersResponse, error) {
	return nil, nil
}
func (f *fakeNodeSandboxService) Stats(context.Context, *runtimev1.StatsRequest) (*runtimev1.StatsResponse, error) {
	return nil, nil
}
func (f *fakeNodeSandboxService) Kill(context.Context, *runtimev1.KillRequest) (*runtimev1.KillResponse, error) {
	return nil, nil
}
func (f *fakeNodeSandboxService) Version(context.Context, *runtimev1.VersionRequest) (*runtimev1.VersionResponse, error) {
	return nil, nil
}
func (f *fakeNodeSandboxService) ReportAllocationLifecycle(allocationID string, status commonv1.AllocationLifecycleState, exitCode *int32, ready bool, readinessMessage string, message string, observedAt time.Time) {
	_ = observedAt
	_ = ready
	_ = readinessMessage
	f.reportedAllocationID = allocationID
	f.reportedStatus = status
	if exitCode != nil {
		f.reportedExitCode = *exitCode
		f.reportedKnown = true
	}
	f.reportedMessage = message
}

func (f *fakeNodeSandboxService) Exec(ctx context.Context, req *runtimev1.ExecRequest) (*runtimev1.ExecResponse, error) {
	_ = ctx
	f.execRequests = append(f.execRequests, req)
	return &runtimev1.ExecResponse{ExitCode: 0, Stdout: []byte("ok\n")}, nil
}

func (f *fakeNodeSandboxService) SandboxCapabilityStatus(ctx context.Context, containerID string) (service.SandboxCapabilityStatus, error) {
	_ = ctx
	f.capabilityStatusIDs = append(f.capabilityStatusIDs, containerID)
	return service.SandboxCapabilityStatus{
		Ready:        true,
		Capabilities: []string{"health", "status", "process", "pty", "computer_use"},
		Providers: []service.SandboxCapabilityProvider{{
			Name:         "computer_use",
			State:        "degraded",
			Available:    true,
			Capabilities: []string{"computer_use"},
			Backend:      "x11",
			Reason:       "window manager degraded",
			Dependencies: []service.SandboxCapabilityRequirement{{
				Name:      "xdotool",
				Available: true,
			}},
		}},
		ProviderSummary: service.SandboxCapabilityProviderSummary{
			Total:    1,
			Degraded: 1,
		},
	}, nil
}

func (f *fakeNodeSandboxService) ComputerUseStatus(ctx context.Context, req *runtimev1.ComputerUseStatusRequest) (*runtimev1.ComputerUseStatusResponse, error) {
	_ = ctx
	f.computerUseStatusReqs = append(f.computerUseStatusReqs, req)
	return &runtimev1.ComputerUseStatusResponse{Available: true, Display: ":99", Backend: "x11"}, nil
}

func (f *fakeNodeSandboxService) ComputerUseScreenshot(ctx context.Context, req *runtimev1.ComputerUseScreenshotRequest) (*runtimev1.ComputerUseScreenshotResponse, error) {
	_ = ctx
	f.computerUseScreenReqs = append(f.computerUseScreenReqs, req)
	return &runtimev1.ComputerUseScreenshotResponse{Data: []byte("png"), ContentType: "image/png"}, nil
}

func (f *fakeNodeSandboxService) ComputerUseDisplay(ctx context.Context, req *runtimev1.ComputerUseDisplayRequest) (*runtimev1.ComputerUseDisplayResponse, error) {
	_ = ctx
	f.computerUseDisplayReqs = append(f.computerUseDisplayReqs, req)
	return &runtimev1.ComputerUseDisplayResponse{Display: ":99", Backend: "x11", Width: 1280, Height: 720}, nil
}

func (f *fakeNodeSandboxService) ComputerUseMouse(ctx context.Context, req *runtimev1.ComputerUseMouseRequest) (*runtimev1.ComputerUseMouseResponse, error) {
	_ = ctx
	f.computerUseMouseReqs = append(f.computerUseMouseReqs, req)
	return &runtimev1.ComputerUseMouseResponse{}, nil
}

func (f *fakeNodeSandboxService) ComputerUseKeyboard(ctx context.Context, req *runtimev1.ComputerUseKeyboardRequest) (*runtimev1.ComputerUseKeyboardResponse, error) {
	_ = ctx
	f.computerUseKeyboardReqs = append(f.computerUseKeyboardReqs, req)
	return &runtimev1.ComputerUseKeyboardResponse{}, nil
}

func (f *fakeNodeSandboxService) StatFile(ctx context.Context, req *runtimev1.StatFileRequest) (*runtimev1.StatFileResponse, error) {
	_ = ctx
	f.statFileRequests = append(f.statFileRequests, req)
	return &runtimev1.StatFileResponse{Info: &filev1.SandboxFileInfo{
		Path:    req.GetPath(),
		Kind:    filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE,
		Size:    5,
		Mode:    0644,
		MtimeNs: 7,
	}}, nil
}

func (f *fakeNodeSandboxService) ListDir(ctx context.Context, req *runtimev1.ListDirRequest) (*runtimev1.ListDirResponse, error) {
	_ = ctx
	f.listDirRequests = append(f.listDirRequests, req)
	return &runtimev1.ListDirResponse{Entries: []*filev1.SandboxFileInfo{{
		Path:    req.GetPath() + "/out.txt",
		Kind:    filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE,
		Size:    5,
		Mode:    0644,
		MtimeNs: 7,
	}}}, nil
}

func (f *fakeNodeSandboxService) ReadFile(ctx context.Context, req *runtimev1.ReadFileRequest) (*runtimev1.ReadFileResponse, error) {
	_ = ctx
	f.readFileRequests = append(f.readFileRequests, req)
	return &runtimev1.ReadFileResponse{Data: []byte("hello")}, nil
}

func (f *fakeNodeSandboxService) WriteFile(ctx context.Context, req *runtimev1.WriteFileRequest) (*runtimev1.WriteFileResponse, error) {
	_ = ctx
	f.writeFileRequests = append(f.writeFileRequests, req)
	return &runtimev1.WriteFileResponse{}, nil
}

func (f *fakeNodeSandboxService) Mkdir(ctx context.Context, req *runtimev1.MkdirRequest) (*runtimev1.MkdirResponse, error) {
	_ = ctx
	f.mkdirRequests = append(f.mkdirRequests, req)
	return &runtimev1.MkdirResponse{}, nil
}

func (f *fakeNodeSandboxService) Remove(ctx context.Context, req *runtimev1.RemoveRequest) (*runtimev1.RemoveResponse, error) {
	_ = ctx
	f.removeRequests = append(f.removeRequests, req)
	return &runtimev1.RemoveResponse{}, nil
}

func (f *fakeNodeSandboxService) Exists(ctx context.Context, req *runtimev1.ExistsRequest) (*runtimev1.ExistsResponse, error) {
	_ = ctx
	f.existsRequests = append(f.existsRequests, req)
	return &runtimev1.ExistsResponse{Exists: true}, nil
}

func (f *fakeNodeSandboxService) Copy(ctx context.Context, req *runtimev1.CopyRequest) (*runtimev1.CopyResponse, error) {
	_ = ctx
	f.copyRequests = append(f.copyRequests, req)
	return &runtimev1.CopyResponse{}, nil
}

func (f *fakeNodeSandboxService) Move(ctx context.Context, req *runtimev1.MoveRequest) (*runtimev1.MoveResponse, error) {
	_ = ctx
	f.moveRequests = append(f.moveRequests, req)
	return &runtimev1.MoveResponse{}, nil
}

func (f *fakeNodeSandboxService) Chmod(ctx context.Context, req *runtimev1.ChmodRequest) (*runtimev1.ChmodResponse, error) {
	_ = ctx
	f.chmodRequests = append(f.chmodRequests, req)
	return &runtimev1.ChmodResponse{}, nil
}

func (f *fakeNodeSandboxService) Touch(ctx context.Context, req *runtimev1.TouchRequest) (*runtimev1.TouchResponse, error) {
	_ = ctx
	f.touchRequests = append(f.touchRequests, req)
	return &runtimev1.TouchResponse{}, nil
}

func (f *fakeNodeSandboxService) UploadArchive(ctx context.Context, req *runtimev1.UploadArchiveRequest, archive io.Reader) (*runtimev1.UploadArchiveResponse, error) {
	_ = ctx
	f.uploadArchiveRequests = append(f.uploadArchiveRequests, req)
	_, _ = io.Copy(io.Discard, archive)
	return &runtimev1.UploadArchiveResponse{}, nil
}

func (f *fakeNodeSandboxService) DownloadArchive(ctx context.Context, req *runtimev1.DownloadArchiveRequest, archive io.Writer) (*runtimev1.DownloadArchiveResponse, error) {
	_ = ctx
	f.downloadArchiveRequests = append(f.downloadArchiveRequests, req)
	_, _ = archive.Write([]byte("archive"))
	return &runtimev1.DownloadArchiveResponse{}, nil
}

func (f *fakeNodeSandboxService) Wait(ctx context.Context, req *runtimev1.WaitRequest) (*runtimev1.WaitResponse, error) {
	_ = ctx
	f.waitRequests = append(f.waitRequests, req)
	return &runtimev1.WaitResponse{ExitCode: func() *int32 { value := int32(17); return &value }(), Message: "done"}, nil
}

type fakeNodeSandboxProcessStream struct {
	ctx      context.Context
	requests []*nodesandboxv1.ProcessRequest
	sent     []*nodesandboxv1.ProcessResponse
	header   metadata.MD
}

func (f *fakeNodeSandboxProcessStream) Send(resp *nodesandboxv1.ProcessResponse) error {
	f.sent = append(f.sent, resp)
	return nil
}

func (f *fakeNodeSandboxProcessStream) Recv() (*nodesandboxv1.ProcessRequest, error) {
	if len(f.requests) == 0 {
		return nil, io.EOF
	}
	req := f.requests[0]
	f.requests = f.requests[1:]
	return req, nil
}

func (f *fakeNodeSandboxProcessStream) SetHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxProcessStream) SendHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxProcessStream) SetTrailer(metadata.MD) {}
func (f *fakeNodeSandboxProcessStream) Context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}
func (f *fakeNodeSandboxProcessStream) SendMsg(any) error { return nil }
func (f *fakeNodeSandboxProcessStream) RecvMsg(any) error { return io.EOF }

type fakeNodeSandboxUploadArchiveStream struct {
	ctx      context.Context
	requests []*nodesandboxv1.UploadArchiveRequest
	closed   *nodesandboxv1.UploadArchiveResponse
	header   metadata.MD
}

func (f *fakeNodeSandboxUploadArchiveStream) Recv() (*nodesandboxv1.UploadArchiveRequest, error) {
	if len(f.requests) == 0 {
		return nil, io.EOF
	}
	req := f.requests[0]
	f.requests = f.requests[1:]
	return req, nil
}

func (f *fakeNodeSandboxUploadArchiveStream) SendAndClose(resp *nodesandboxv1.UploadArchiveResponse) error {
	f.closed = resp
	return nil
}

func (f *fakeNodeSandboxUploadArchiveStream) SetHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxUploadArchiveStream) SendHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxUploadArchiveStream) SetTrailer(metadata.MD) {}
func (f *fakeNodeSandboxUploadArchiveStream) Context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}
func (f *fakeNodeSandboxUploadArchiveStream) SendMsg(any) error { return nil }
func (f *fakeNodeSandboxUploadArchiveStream) RecvMsg(any) error { return io.EOF }

type fakeNodeSandboxDownloadArchiveStream struct {
	ctx    context.Context
	sent   []*nodesandboxv1.DownloadArchiveResponse
	header metadata.MD
}

func (f *fakeNodeSandboxDownloadArchiveStream) Send(resp *nodesandboxv1.DownloadArchiveResponse) error {
	f.sent = append(f.sent, resp)
	return nil
}

func (f *fakeNodeSandboxDownloadArchiveStream) SetHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxDownloadArchiveStream) SendHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxDownloadArchiveStream) SetTrailer(metadata.MD) {}
func (f *fakeNodeSandboxDownloadArchiveStream) Context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}
func (f *fakeNodeSandboxDownloadArchiveStream) SendMsg(any) error { return nil }
func (f *fakeNodeSandboxDownloadArchiveStream) RecvMsg(any) error { return io.EOF }

func TestLocalNodeSandboxRejectsControlPlaneAllocation(t *testing.T) {
	t.Parallel()
	service := &fakeNodeSandboxService{controlPlaneIDs: map[string]bool{"alloc-bound": true}}
	server := &nodeSandboxServer{svc: service, nodeID: "node-a", localOnly: true}
	_, err := server.validateDirectAuth(allocationAccessIncomingContext(context.Background(), "verify-local-access"), "alloc-bound")
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("validateDirectAuth() code = %v, want permission denied", grpcstatus.Code(err))
	}
}

func TestNodeSandboxExecBridgesRequest(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", testAccessGrantValidator{})

	resp, err := server.Exec(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ExecRequest{
		AllocationID: "alloc-123",
		Spec: &nodesandboxv1.ExecSpec{
			Argv:           []string{"python", "-c", "print('ok')"},
			Env:            map[string]string{"A": "B"},
			Cwd:            "/workspace",
			User:           "axern",
			TimeoutSeconds: 9,
		},
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if resp.GetExitCode() != 0 || string(resp.GetStdout()) != "ok\n" {
		t.Fatalf("unexpected exec response = %#v", resp)
	}
	if len(fakeService.execRequests) != 1 {
		t.Fatalf("exec request count = %d, want 1", len(fakeService.execRequests))
	}
	got := fakeService.execRequests[0]
	if got.GetID() != "alloc-123" {
		t.Fatalf("exec request id = %q, want alloc-123", got.GetID())
	}
	if got.GetCwd() != "/workspace" {
		t.Fatalf("exec request cwd = %q, want /workspace", got.GetCwd())
	}
	if got.GetTimeout() != 9 {
		t.Fatalf("exec request timeout = %d, want 9", got.GetTimeout())
	}
	if got.GetEnv()["A"] != "B" {
		t.Fatalf("exec request env = %#v, want key A", got.GetEnv())
	}
	if got.GetUser() != "axern" {
		t.Fatalf("exec request user = %q, want axern", got.GetUser())
	}
}

func TestNodeSandboxProcessBridgesStream(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{
		processFunc: func(stream service.ProcessStreamServer) error {
			open, err := stream.Recv()
			if err != nil {
				return err
			}
			if open.GetOpen().GetID() != "alloc-123" || open.GetOpen().GetCommand()[0] != "/bin/sh" {
				t.Fatalf("unexpected process open = %#v", open.GetOpen())
			}
			if size := open.GetOpen().GetInitialSize(); size.GetCols() != 120 || size.GetRows() != 40 {
				t.Fatalf("unexpected initial terminal size = %#v", size)
			}
			next, err := stream.Recv()
			if err != nil {
				return err
			}
			if string(next.GetStdin()) != "payload" {
				t.Fatalf("unexpected process stdin = %#v", next)
			}
			return stream.Send(&runtimev1.ProcessResponse{Payload: &runtimev1.ProcessResponse_Exit{Exit: &runtimev1.ExecExit{ExitCode: 0}}})
		},
	}
	server := NewNodeSandboxServer(fakeService, "node-a", testAccessGrantValidator{})
	stream := &fakeNodeSandboxProcessStream{
		ctx: allocationAccessIncomingContext(context.Background(), "lease-token"),
		requests: []*nodesandboxv1.ProcessRequest{
			{Payload: &nodesandboxv1.ProcessRequest_Open{Open: &nodesandboxv1.ProcessOpen{
				AllocationID: "alloc-123",
				Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"/bin/sh"}, Tty: true},
				InitialSize:  &nodesandboxv1.TerminalResize{Cols: 120, Rows: 40},
			}}},
			{Payload: &nodesandboxv1.ProcessRequest_Stdin{Stdin: []byte("payload")}},
		},
	}

	if err := server.Process(stream); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetExit().GetExitCode() != 0 {
		t.Fatalf("unexpected process responses = %#v", stream.sent)
	}
	assertAllocationAccessGrantAccepted(t, stream.header)
}

func TestNodeSandboxArchiveStreamsAcknowledgeLease(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", testAccessGrantValidator{})
	upload := &fakeNodeSandboxUploadArchiveStream{ctx: allocationAccessIncomingContext(context.Background(), "lease-token"), requests: []*nodesandboxv1.UploadArchiveRequest{
		{Payload: &nodesandboxv1.UploadArchiveRequest_Open{Open: &nodesandboxv1.UploadArchiveOpen{
			AllocationID: "alloc-123", Path: "/workspace",
			Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
			SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
		}}},
		{Payload: &nodesandboxv1.UploadArchiveRequest_Chunk{Chunk: []byte("archive")}},
	}}
	if err := server.UploadArchive(upload); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}
	assertAllocationAccessGrantAccepted(t, upload.header)
	if upload.closed == nil || len(fakeService.uploadArchiveRequests) != 1 {
		t.Fatalf("upload result = %#v requests = %#v", upload.closed, fakeService.uploadArchiveRequests)
	}

	download := &fakeNodeSandboxDownloadArchiveStream{ctx: allocationAccessIncomingContext(context.Background(), "lease-token")}
	if err := server.DownloadArchive(&nodesandboxv1.DownloadArchiveRequest{
		AllocationID: "alloc-123", Path: "/workspace",
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, download); err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	assertAllocationAccessGrantAccepted(t, download.header)
	if len(download.sent) != 1 || string(download.sent[0].GetChunk()) != "archive" {
		t.Fatalf("download responses = %#v", download.sent)
	}
}

func assertAllocationAccessGrantAccepted(t *testing.T, header metadata.MD) {
	t.Helper()
	if got := header.Get(accessGrantAcceptedHeaderKey); len(got) != 1 || got[0] != "1" {
		t.Fatalf("allocation access grant acceptance header = %#v, want 1", got)
	}
}

func TestNodeSandboxFileMetadataBridgesRequests(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", testAccessGrantValidator{})

	statResp, err := server.StatFile(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.StatFileRequest{
		AllocationID: "alloc-123",
		Path:         "/tmp/out.txt",
	})
	if err != nil {
		t.Fatalf("StatFile() error = %v", err)
	}
	if statResp.GetInfo().GetPath() != "/tmp/out.txt" || statResp.GetInfo().GetKind() != filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE {
		t.Fatalf("unexpected stat response = %#v", statResp.GetInfo())
	}
	if len(fakeService.statFileRequests) != 1 || fakeService.statFileRequests[0].GetID() != "alloc-123" {
		t.Fatalf("stat request = %#v", fakeService.statFileRequests)
	}

	listResp, err := server.ListDir(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ListDirRequest{
		AllocationID: "alloc-123",
		Path:         "/tmp",
	})
	if err != nil {
		t.Fatalf("ListDir() error = %v", err)
	}
	if len(listResp.GetEntries()) != 1 || listResp.GetEntries()[0].GetPath() != "/tmp/out.txt" {
		t.Fatalf("unexpected list response = %#v", listResp.GetEntries())
	}
	if len(fakeService.listDirRequests) != 1 || fakeService.listDirRequests[0].GetID() != "alloc-123" {
		t.Fatalf("list request = %#v", fakeService.listDirRequests)
	}

	readResp, err := server.ReadFile(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ReadFileRequest{
		AllocationID: "alloc-123",
		Path:         "/tmp/out.txt",
	})
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(readResp.GetData()) != "hello" || len(fakeService.readFileRequests) != 1 || fakeService.readFileRequests[0].GetPath() != "/tmp/out.txt" {
		t.Fatalf("read response/request = response=%q requests=%#v", string(readResp.GetData()), fakeService.readFileRequests)
	}

	_, err = server.WriteFile(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.WriteFileRequest{
		AllocationID:  "alloc-123",
		Path:          "/tmp/out.txt",
		Data:          []byte("hello"),
		CreateParents: true,
	})
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if len(fakeService.writeFileRequests) != 1 || string(fakeService.writeFileRequests[0].GetData()) != "hello" || !fakeService.writeFileRequests[0].GetCreateParents() {
		t.Fatalf("write request = %#v", fakeService.writeFileRequests)
	}

	_, err = server.Mkdir(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.MkdirRequest{
		AllocationID: "alloc-123",
		Path:         "/tmp/nested",
		Parents:      true,
	})
	if err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if len(fakeService.mkdirRequests) != 1 || !fakeService.mkdirRequests[0].GetParents() {
		t.Fatalf("mkdir request = %#v", fakeService.mkdirRequests)
	}

	_, err = server.Remove(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.RemoveRequest{
		AllocationID: "alloc-123",
		Path:         "/tmp/nested",
		Recursive:    true,
		Force:        true,
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(fakeService.removeRequests) != 1 || !fakeService.removeRequests[0].GetRecursive() || !fakeService.removeRequests[0].GetForce() {
		t.Fatalf("remove request = %#v", fakeService.removeRequests)
	}

	existsResp, err := server.Exists(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ExistsRequest{
		AllocationID: "alloc-123",
		Path:         "/tmp/out.txt",
	})
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !existsResp.GetExists() || len(fakeService.existsRequests) != 1 {
		t.Fatalf("exists response/request = response=%v requests=%#v", existsResp.GetExists(), fakeService.existsRequests)
	}

	_, err = server.Copy(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.CopyRequest{
		AllocationID: "alloc-123",
		SrcPath:      "/tmp/out.txt",
		DstPath:      "/tmp/copy.txt",
		Recursive:    true,
		Overwrite:    true,
	})
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if len(fakeService.copyRequests) != 1 || fakeService.copyRequests[0].GetDstPath() != "/tmp/copy.txt" {
		t.Fatalf("copy request = %#v", fakeService.copyRequests)
	}

	_, err = server.Move(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.MoveRequest{
		AllocationID: "alloc-123",
		SrcPath:      "/tmp/copy.txt",
		DstPath:      "/tmp/moved.txt",
		Overwrite:    true,
	})
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if len(fakeService.moveRequests) != 1 || fakeService.moveRequests[0].GetDstPath() != "/tmp/moved.txt" {
		t.Fatalf("move request = %#v", fakeService.moveRequests)
	}

	_, err = server.Chmod(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ChmodRequest{
		AllocationID: "alloc-123",
		Path:         "/tmp/moved.txt",
		Mode:         0600,
		Recursive:    true,
	})
	if err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if len(fakeService.chmodRequests) != 1 || fakeService.chmodRequests[0].GetMode() != 0600 {
		t.Fatalf("chmod request = %#v", fakeService.chmodRequests)
	}

	_, err = server.Touch(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.TouchRequest{
		AllocationID: "alloc-123",
		Path:         "/tmp/moved.txt",
		Create:       true,
		MtimeNs:      7,
	})
	if err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	if len(fakeService.touchRequests) != 1 || fakeService.touchRequests[0].GetMtimeNs() != 7 {
		t.Fatalf("touch request = %#v", fakeService.touchRequests)
	}
}

func TestNodeSandboxArchiveBridgesRequests(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", testAccessGrantValidator{})

	uploadStream := &fakeNodeSandboxUploadArchiveStream{ctx: allocationAccessIncomingContext(context.Background(), "lease-token"), requests: []*nodesandboxv1.UploadArchiveRequest{
		{
			Payload: &nodesandboxv1.UploadArchiveRequest_Open{Open: &nodesandboxv1.UploadArchiveOpen{
				AllocationID:  "alloc-123",
				Path:          "/tmp/tree",
				Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
				CreateParents: true,
				Overwrite:     true,
				SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
			}},
		},
		{Payload: &nodesandboxv1.UploadArchiveRequest_Chunk{Chunk: []byte("archive")}},
	}}
	if err := server.UploadArchive(uploadStream); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}
	if uploadStream.closed == nil || len(fakeService.uploadArchiveRequests) != 1 {
		t.Fatalf("upload close/request = closed=%v requests=%#v", uploadStream.closed, fakeService.uploadArchiveRequests)
	}
	if got := fakeService.uploadArchiveRequests[0]; got.GetID() != "alloc-123" || got.GetPath() != "/tmp/tree" || !got.GetCreateParents() || !got.GetOverwrite() {
		t.Fatalf("upload request = %#v", got)
	}

	downloadStream := &fakeNodeSandboxDownloadArchiveStream{ctx: allocationAccessIncomingContext(context.Background(), "lease-token")}
	err := server.DownloadArchive(&nodesandboxv1.DownloadArchiveRequest{
		AllocationID:  "alloc-123",
		Path:          "/tmp/tree",
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, downloadStream)
	if err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	if len(downloadStream.sent) != 1 || string(downloadStream.sent[0].GetChunk()) != "archive" {
		t.Fatalf("download stream sent = %#v", downloadStream.sent)
	}
	if len(fakeService.downloadArchiveRequests) != 1 || fakeService.downloadArchiveRequests[0].GetPath() != "/tmp/tree" {
		t.Fatalf("download request = %#v", fakeService.downloadArchiveRequests)
	}
}

func TestNodeSandboxCapabilityStatusBridgesSafeSummary(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", testAccessGrantValidator{})

	resp, err := server.CapabilityStatus(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.CapabilityStatusRequest{
		AllocationID: "alloc-123",
	})
	if err != nil {
		t.Fatalf("CapabilityStatus() error = %v", err)
	}
	if !resp.GetReady() {
		t.Fatal("ready = false, want true")
	}
	if got := fakeService.capabilityStatusIDs; len(got) != 1 || got[0] != "alloc-123" {
		t.Fatalf("capability status ids = %v", got)
	}
	if got := resp.GetCapabilities(); len(got) != 5 || got[4] != "computer_use" {
		t.Fatalf("capabilities = %v", got)
	}
	if got := resp.GetProviderSummary(); got.GetTotal() != 1 || got.GetDegraded() != 1 {
		t.Fatalf("provider summary = %#v", got)
	}
	providers := resp.GetProviders()
	if len(providers) != 1 {
		t.Fatalf("providers = %#v", providers)
	}
	provider := providers[0]
	if provider.GetName() != "computer_use" || provider.GetState() != "degraded" || !provider.GetAvailable() || provider.GetBackend() != "x11" {
		t.Fatalf("provider = %#v", provider)
	}
	if len(provider.GetDependencies()) != 1 || provider.GetDependencies()[0].GetName() != "xdotool" {
		t.Fatalf("provider dependencies = %#v", provider.GetDependencies())
	}
}

func TestNodeSandboxComputerUseBridgesRequests(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", testAccessGrantValidator{})
	statusResp, err := server.ComputerUseStatus(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ComputerUseStatusRequest{
		AllocationID: "alloc-123",
	})
	if err != nil {
		t.Fatalf("ComputerUseStatus() error = %v", err)
	}
	if !statusResp.GetAvailable() || statusResp.GetDisplay() != ":99" || statusResp.GetBackend() != "x11" {
		t.Fatalf("status response = %#v", statusResp)
	}
	screenResp, err := server.ComputerUseScreenshot(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ComputerUseScreenshotRequest{
		AllocationID: "alloc-123",
		Region:       &nodesandboxv1.ComputerUseRegion{X: 1, Y: 2, Width: 3, Height: 4},
		Format:       "jpeg",
		Quality:      75,
		Scale:        0.5,
	})
	if err != nil {
		t.Fatalf("ComputerUseScreenshot() error = %v", err)
	}
	if string(screenResp.GetData()) != "png" || screenResp.GetContentType() != "image/png" {
		t.Fatalf("screenshot response = %#v", screenResp)
	}
	if len(fakeService.computerUseStatusReqs) != 1 || fakeService.computerUseStatusReqs[0].GetID() != "alloc-123" {
		t.Fatalf("status requests = %#v", fakeService.computerUseStatusReqs)
	}
	if len(fakeService.computerUseScreenReqs) != 1 || fakeService.computerUseScreenReqs[0].GetID() != "alloc-123" {
		t.Fatalf("screenshot requests = %#v", fakeService.computerUseScreenReqs)
	}
	if got := fakeService.computerUseScreenReqs[0]; got.GetRegion().GetWidth() != 3 || got.GetFormat() != "jpeg" || got.GetQuality() != 75 || got.GetScale() != 0.5 {
		t.Fatalf("screenshot request details = %#v", got)
	}
	displayResp, err := server.ComputerUseDisplay(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ComputerUseDisplayRequest{
		AllocationID: "alloc-123",
	})
	if err != nil {
		t.Fatalf("ComputerUseDisplay() error = %v", err)
	}
	if displayResp.GetWidth() != 1280 || displayResp.GetHeight() != 720 {
		t.Fatalf("display response = %#v", displayResp)
	}
	if _, err := server.ComputerUseMouse(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ComputerUseMouseRequest{
		AllocationID: "alloc-123",
		Action:       "click",
		X:            7,
		Y:            9,
		Button:       "1",
	}); err != nil {
		t.Fatalf("ComputerUseMouse() error = %v", err)
	}
	if _, err := server.ComputerUseKeyboard(allocationAccessIncomingContext(context.Background(), "lease-token"), &nodesandboxv1.ComputerUseKeyboardRequest{
		AllocationID: "alloc-123",
		Text:         "hello",
	}); err != nil {
		t.Fatalf("ComputerUseKeyboard() error = %v", err)
	}
	if len(fakeService.computerUseDisplayReqs) != 1 || len(fakeService.computerUseMouseReqs) != 1 || len(fakeService.computerUseKeyboardReqs) != 1 {
		t.Fatalf("computer-use requests display=%d mouse=%d keyboard=%d", len(fakeService.computerUseDisplayReqs), len(fakeService.computerUseMouseReqs), len(fakeService.computerUseKeyboardReqs))
	}
}

func TestNodeSandboxUploadArchiveRequiresOpenFrame(t *testing.T) {
	t.Parallel()

	server := NewNodeSandboxServer(&fakeNodeSandboxService{}, "node-a", testAccessGrantValidator{})
	err := server.UploadArchive(&fakeNodeSandboxUploadArchiveStream{requests: []*nodesandboxv1.UploadArchiveRequest{
		{Payload: &nodesandboxv1.UploadArchiveRequest_Chunk{Chunk: []byte("archive")}},
	}})

	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("UploadArchive() code = %v, want InvalidArgument", grpcstatus.Code(err))
	}
}

func TestNodeSandboxUploadArchiveRequiresNonEmptyStream(t *testing.T) {
	t.Parallel()

	server := NewNodeSandboxServer(&fakeNodeSandboxService{}, "node-a", testAccessGrantValidator{})
	err := server.UploadArchive(&fakeNodeSandboxUploadArchiveStream{})

	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("UploadArchive() code = %v, want InvalidArgument", grpcstatus.Code(err))
	}
}

func TestNodeSandboxUploadArchiveDoesNotAcknowledgeRejectedLease(t *testing.T) {
	t.Parallel()

	server := NewNodeSandboxServer(&fakeNodeSandboxService{}, "node-a", testAccessGrantValidator{})
	stream := &fakeNodeSandboxUploadArchiveStream{requests: []*nodesandboxv1.UploadArchiveRequest{
		{Payload: &nodesandboxv1.UploadArchiveRequest_Open{Open: &nodesandboxv1.UploadArchiveOpen{
			AllocationID: "alloc-123", Path: "/workspace",
			Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
			SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
		}}},
	}}
	err := server.UploadArchive(stream)

	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("UploadArchive() code = %v, want Unauthenticated", grpcstatus.Code(err))
	}
	if got := stream.header.Get(accessGrantAcceptedHeaderKey); len(got) != 0 {
		t.Fatalf("rejected access grant acceptance header = %#v, want none", got)
	}
}

func TestNodeSandboxExecRequiresAllocationLease(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", testAccessGrantValidator{})

	_, err := server.Exec(context.Background(), &nodesandboxv1.ExecRequest{
		AllocationID: "alloc-123",
		Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"true"}},
	})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Exec() error code = %v, want %v", grpcstatus.Code(err), codes.Unauthenticated)
	}
}

func TestNodeSandboxExecRejectsAmbiguousLeaseMetadata(t *testing.T) {
	t.Parallel()

	server := NewNodeSandboxServer(&fakeNodeSandboxService{}, "node-a", testAccessGrantValidator{})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		accessGrantTokenMetadataKey, "grant-one",
		accessGrantTokenMetadataKey, "grant-two",
	))
	_, err := server.Exec(ctx, &nodesandboxv1.ExecRequest{
		AllocationID: "alloc-123",
		Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"true"}},
	})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Exec() error code = %v, want %v", grpcstatus.Code(err), codes.Unauthenticated)
	}
}

func TestNodeSandboxExecAcceptsAccessGrantCacheTokenHash(t *testing.T) {
	t.Parallel()

	token := "access-token"
	sum := sha256.Sum256([]byte(token))
	cache := controlplane.NewAccessGrantCache()
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{Revision: 1, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE,
		GrantID:             "grant-123",
		AllocationID:        "alloc-123",
		ValidationTokenHash: hex.EncodeToString(sum[:]),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
	}})
	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", cache)

	_, err := server.Exec(allocationAccessIncomingContext(context.Background(), "access-token"), &nodesandboxv1.ExecRequest{
		AllocationID: "alloc-123",
		Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"true"}},
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
}

func TestNodeSandboxExecRejectsRevokedAccessGrantFromCache(t *testing.T) {
	t.Parallel()

	token := "access-token"
	sum := sha256.Sum256([]byte(token))
	cache := controlplane.NewAccessGrantCache()
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{Revision: 1, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE,
		AllocationID:        "alloc-123",
		ValidationTokenHash: hex.EncodeToString(sum[:]),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
		Revoked:             true,
	}})
	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", cache)

	_, err := server.Exec(allocationAccessIncomingContext(context.Background(), "access-token"), &nodesandboxv1.ExecRequest{
		AllocationID: "alloc-123",
		Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"true"}},
	})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Exec() error code = %v, want %v", grpcstatus.Code(err), codes.Unauthenticated)
	}
}

type testAccessGrantValidator struct{}

func (testAccessGrantValidator) WaitValidate(context.Context, string, string, gatewayv1.AllocationAccessPurpose, func() time.Time) (bool, bool) {
	return true, false
}

func TestProductionSandboxRejectsMissingGrantValidator(t *testing.T) {
	server := NewNodeSandboxServer(&fakeNodeSandboxService{}, "node-a", nil).(*nodeSandboxServer)
	_, err := server.validateDirectAuth(allocationAccessIncomingContext(context.Background(), "token"), "allocation-one")
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing validator allowed access: %v", err)
	}
}
