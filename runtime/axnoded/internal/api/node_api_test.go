package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
	"time"

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
	execImageRequests       []*runtimev1.ExecImageRequest
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
	browserStatusReqs       []*runtimev1.BrowserStatusRequest
	browserOpenReqs         []*runtimev1.BrowserOpenRequest
	browserCloseReqs        []*runtimev1.BrowserCloseRequest
	browserNavigateReqs     []*runtimev1.BrowserNavigateRequest
	browserResizeReqs       []*runtimev1.BrowserResizeRequest
	browserClickReqs        []*runtimev1.BrowserClickRequest
	browserTypeReqs         []*runtimev1.BrowserTypeRequest
	browserWaitReqs         []*runtimev1.BrowserWaitRequest
	capabilityStatusIDs     []string
	waitRequests            []*runtimev1.WaitRequest
	reportedAllocationID    string
	reportedAttempt         int64
	reportedStatus          commonv1.AllocationStatus
	reportedExitCode        int32
	reportedKnown           bool
	reportedMessage         string
	execStreamFunc          func(service.ExecStreamServer) error
	processFunc             func(service.ProcessStreamServer) error
	processImageFunc        func(service.ProcessImageStreamServer) error
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
	if f.execStreamFunc != nil {
		return f.execStreamFunc(stream)
	}
	return nil
}
func (f *fakeNodeSandboxService) Process(stream service.ProcessStreamServer) error {
	if f.processFunc != nil {
		return f.processFunc(stream)
	}
	return nil
}
func (f *fakeNodeSandboxService) ProcessImage(stream service.ProcessImageStreamServer) error {
	if f.processImageFunc != nil {
		return f.processImageFunc(stream)
	}
	return nil
}
func (f *fakeNodeSandboxService) ProxyHTTP(service.HTTPProxyServer) error  { return nil }
func (f *fakeNodeSandboxService) Ready() bool                              { return true }
func (f *fakeNodeSandboxService) RuntimeStatuses() []service.RuntimeStatus { return nil }
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
func (f *fakeNodeSandboxService) Checkpoint(context.Context, *runtimev1.CheckpointRequest) (*runtimev1.CheckpointResponse, error) {
	return nil, nil
}
func (f *fakeNodeSandboxService) Version(context.Context, *runtimev1.VersionRequest) (*runtimev1.VersionResponse, error) {
	return nil, nil
}
func (f *fakeNodeSandboxService) ReportAllocationStatus(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time) {
	_ = observedAt
	_ = ready
	_ = readinessMessage
	f.reportedAllocationID = allocationID
	f.reportedAttempt = attempt
	f.reportedStatus = status
	f.reportedExitCode = exitCode
	f.reportedKnown = exitCodeKnown
	f.reportedMessage = message
}

func (f *fakeNodeSandboxService) Exec(ctx context.Context, req *runtimev1.ExecRequest) (*runtimev1.ExecResponse, error) {
	_ = ctx
	f.execRequests = append(f.execRequests, req)
	return &runtimev1.ExecResponse{ExitCode: 0, Stdout: []byte("ok\n")}, nil
}

func (f *fakeNodeSandboxService) ExecImage(ctx context.Context, req *runtimev1.ExecImageRequest) (*runtimev1.ExecImageResponse, error) {
	_ = ctx
	f.execImageRequests = append(f.execImageRequests, req)
	return &runtimev1.ExecImageResponse{ExitCode: 0, Stdout: []byte("image-ok\n")}, nil
}

func (f *fakeNodeSandboxService) SandboxCapabilityStatus(ctx context.Context, containerID string) (service.SandboxCapabilityStatus, error) {
	_ = ctx
	f.capabilityStatusIDs = append(f.capabilityStatusIDs, containerID)
	return service.SandboxCapabilityStatus{
		Ready:        true,
		Capabilities: []string{"health", "status", "process", "pty", "browser"},
		Providers: []service.SandboxCapabilityProvider{{
			Name:         "browser",
			State:        "degraded",
			Available:    true,
			Capabilities: []string{"browser"},
			Backend:      "chromium",
			Reason:       "window manager degraded",
			Dependencies: []service.SandboxCapabilityDependency{{
				Name:      "chromium",
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

func (f *fakeNodeSandboxService) BrowserStatus(ctx context.Context, req *runtimev1.BrowserStatusRequest) (*runtimev1.BrowserStatusResponse, error) {
	_ = ctx
	f.browserStatusReqs = append(f.browserStatusReqs, req)
	return &runtimev1.BrowserStatusResponse{Available: true, Command: "chromium", Running: false}, nil
}

func (f *fakeNodeSandboxService) BrowserOpen(ctx context.Context, req *runtimev1.BrowserOpenRequest) (*runtimev1.BrowserStatusResponse, error) {
	_ = ctx
	f.browserOpenReqs = append(f.browserOpenReqs, req)
	return &runtimev1.BrowserStatusResponse{Available: true, Command: "chromium", Running: true, Pid: 99, Url: req.GetUrl()}, nil
}

func (f *fakeNodeSandboxService) BrowserClose(ctx context.Context, req *runtimev1.BrowserCloseRequest) (*runtimev1.BrowserStatusResponse, error) {
	_ = ctx
	f.browserCloseReqs = append(f.browserCloseReqs, req)
	return &runtimev1.BrowserStatusResponse{Available: true, Command: "chromium", Running: false}, nil
}

func (f *fakeNodeSandboxService) BrowserNavigate(ctx context.Context, req *runtimev1.BrowserNavigateRequest) (*runtimev1.BrowserStatusResponse, error) {
	_ = ctx
	f.browserNavigateReqs = append(f.browserNavigateReqs, req)
	return &runtimev1.BrowserStatusResponse{Available: true, Command: "chromium", Running: true, Pid: 99, Url: req.GetUrl()}, nil
}

func (f *fakeNodeSandboxService) BrowserResize(ctx context.Context, req *runtimev1.BrowserResizeRequest) (*runtimev1.BrowserStatusResponse, error) {
	_ = ctx
	f.browserResizeReqs = append(f.browserResizeReqs, req)
	return &runtimev1.BrowserStatusResponse{Available: true, Command: "chromium", Running: true, Pid: 99}, nil
}

func (f *fakeNodeSandboxService) BrowserClick(ctx context.Context, req *runtimev1.BrowserClickRequest) (*runtimev1.BrowserStatusResponse, error) {
	_ = ctx
	f.browserClickReqs = append(f.browserClickReqs, req)
	return &runtimev1.BrowserStatusResponse{Available: true, Command: "chromium", Running: true, Pid: 99}, nil
}

func (f *fakeNodeSandboxService) BrowserType(ctx context.Context, req *runtimev1.BrowserTypeRequest) (*runtimev1.BrowserStatusResponse, error) {
	_ = ctx
	f.browserTypeReqs = append(f.browserTypeReqs, req)
	return &runtimev1.BrowserStatusResponse{Available: true, Command: "chromium", Running: true, Pid: 99}, nil
}

func (f *fakeNodeSandboxService) BrowserWait(ctx context.Context, req *runtimev1.BrowserWaitRequest) (*runtimev1.BrowserStatusResponse, error) {
	_ = ctx
	f.browserWaitReqs = append(f.browserWaitReqs, req)
	return &runtimev1.BrowserStatusResponse{Available: true, Command: "chromium", Running: true, Pid: 99}, nil
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
	return &runtimev1.WaitResponse{Status: 0, ExitCode: 17, Message: "done"}, nil
}

type fakeNodeSandboxExecStream struct {
	ctx      context.Context
	requests []*nodesandboxv1.ExecStreamRequest
	sent     []*nodesandboxv1.ExecStreamResponse
	header   metadata.MD
}

func (f *fakeNodeSandboxExecStream) Send(resp *nodesandboxv1.ExecStreamResponse) error {
	f.sent = append(f.sent, resp)
	return nil
}

func (f *fakeNodeSandboxExecStream) Recv() (*nodesandboxv1.ExecStreamRequest, error) {
	if len(f.requests) == 0 {
		return nil, io.EOF
	}
	req := f.requests[0]
	f.requests = f.requests[1:]
	return req, nil
}

func (f *fakeNodeSandboxExecStream) SetHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxExecStream) SendHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxExecStream) SetTrailer(metadata.MD) {}
func (f *fakeNodeSandboxExecStream) Context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}
func (f *fakeNodeSandboxExecStream) SendMsg(any) error { return nil }
func (f *fakeNodeSandboxExecStream) RecvMsg(any) error { return io.EOF }

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

type fakeNodeSandboxProcessImageStream struct {
	ctx      context.Context
	requests []*nodesandboxv1.ProcessImageRequest
	sent     []*nodesandboxv1.ProcessImageResponse
	header   metadata.MD
}

func (f *fakeNodeSandboxProcessImageStream) Send(resp *nodesandboxv1.ProcessImageResponse) error {
	f.sent = append(f.sent, resp)
	return nil
}

func (f *fakeNodeSandboxProcessImageStream) Recv() (*nodesandboxv1.ProcessImageRequest, error) {
	if len(f.requests) == 0 {
		return nil, io.EOF
	}
	req := f.requests[0]
	f.requests = f.requests[1:]
	return req, nil
}

func (f *fakeNodeSandboxProcessImageStream) SetHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxProcessImageStream) SendHeader(md metadata.MD) error {
	f.header = metadata.Join(f.header, md)
	return nil
}
func (f *fakeNodeSandboxProcessImageStream) SetTrailer(metadata.MD) {}
func (f *fakeNodeSandboxProcessImageStream) Context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}
func (f *fakeNodeSandboxProcessImageStream) SendMsg(any) error { return nil }
func (f *fakeNodeSandboxProcessImageStream) RecvMsg(any) error { return io.EOF }

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

func TestNodeSandboxExecBridgesRequest(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())

	resp, err := server.Exec(context.Background(), &nodesandboxv1.ExecRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
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

func TestNodeSandboxExecImageBridgesRequest(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())

	resp, err := server.ExecImage(context.Background(), &nodesandboxv1.ExecImageRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Spec: &nodesandboxv1.ImageProcessSpec{
			Image:          "ghcr.io/cofy-x/agent:latest",
			Argv:           []string{"tool", "run"},
			Env:            map[string]string{"A": "B"},
			Cwd:            "/workspace",
			User:           "axern",
			TimeoutSeconds: 9,
			Mounts: []*nodesandboxv1.ImageProcessMount{{
				SandboxPath: "/workspace",
				TargetPath:  "/workspace",
				Readonly:    true,
				Options:     []string{"rshared"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("ExecImage() error = %v", err)
	}
	if resp.GetExitCode() != 0 || string(resp.GetStdout()) != "image-ok\n" {
		t.Fatalf("unexpected exec image response = %#v", resp)
	}
	if len(fakeService.execImageRequests) != 1 {
		t.Fatalf("exec image request count = %d, want 1", len(fakeService.execImageRequests))
	}
	got := fakeService.execImageRequests[0]
	if got.GetID() != "alloc-123" {
		t.Fatalf("exec image request id = %q, want alloc-123", got.GetID())
	}
	if got.GetSpec().GetImage() != "ghcr.io/cofy-x/agent:latest" {
		t.Fatalf("exec image image = %q", got.GetSpec().GetImage())
	}
	if got.GetSpec().GetCommand()[0] != "tool" || got.GetSpec().GetCwd() != "/workspace" || got.GetSpec().GetTimeout() != 9 {
		t.Fatalf("unexpected exec image spec = %#v", got.GetSpec())
	}
	if got.GetSpec().GetEnv()["A"] != "B" || got.GetSpec().GetUser() != "axern" {
		t.Fatalf("unexpected exec image env/user = %#v", got.GetSpec())
	}
	mount := got.GetSpec().GetMounts()[0]
	if mount.GetSandboxPath() != "/workspace" || mount.GetTargetPath() != "/workspace" || !mount.GetReadonly() || mount.GetOptions()[0] != "rshared" {
		t.Fatalf("unexpected exec image mount = %#v", mount)
	}
}

func TestNodeSandboxExecStreamExitDoesNotReportAllocationExit(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{
		execStreamFunc: func(stream service.ExecStreamServer) error {
			req, err := stream.Recv()
			if err != nil {
				return err
			}
			if req.GetOpen().GetID() != "alloc-123" {
				t.Fatalf("exec stream target id = %q, want alloc-123", req.GetOpen().GetID())
			}
			if req.GetOpen().GetUser() != "axern" {
				t.Fatalf("exec stream user = %q, want axern", req.GetOpen().GetUser())
			}
			return stream.Send(&runtimev1.ExecStreamResponse{
				Payload: &runtimev1.ExecStreamResponse_Exit{Exit: &runtimev1.ExecExit{
					ExitCode: 0,
					Message:  "exec session done",
				}},
			})
		},
	}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())
	stream := &fakeNodeSandboxExecStream{
		requests: []*nodesandboxv1.ExecStreamRequest{{
			Payload: &nodesandboxv1.ExecStreamRequest_Open{Open: &nodesandboxv1.ExecStreamOpen{
				AllocationID:        "alloc-123",
				Attempt:             1,
				ExecutionLeaseToken: "lease-token",
				Spec:                &nodesandboxv1.ExecSpec{Argv: []string{"/bin/sh"}, Tty: true, User: "axern"},
			}},
		}},
	}

	if err := server.ExecStream(stream); err != nil {
		t.Fatalf("ExecStream() error = %v", err)
	}
	if fakeService.reportedAllocationID != "" {
		t.Fatalf("ExecStream reported allocation exit for %q", fakeService.reportedAllocationID)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetExit().GetMessage() != "exec session done" {
		t.Fatalf("unexpected exec stream responses = %#v", stream.sent)
	}
	if got := stream.header.Get(executionLeaseAcceptedHeaderKey); len(got) != 1 || got[0] != "1" {
		t.Fatalf("execution lease acceptance header = %#v, want 1", got)
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
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())
	stream := &fakeNodeSandboxProcessStream{
		requests: []*nodesandboxv1.ProcessRequest{
			{Payload: &nodesandboxv1.ProcessRequest_Open{Open: &nodesandboxv1.ProcessOpen{
				AllocationID:        "alloc-123",
				Attempt:             1,
				ExecutionLeaseToken: "lease-token",
				Spec:                &nodesandboxv1.ExecSpec{Argv: []string{"/bin/sh"}, Tty: true},
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
	assertExecutionLeaseAccepted(t, stream.header)
}

func TestNodeSandboxArchiveStreamsAcknowledgeLease(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())
	upload := &fakeNodeSandboxUploadArchiveStream{requests: []*nodesandboxv1.UploadArchiveRequest{
		{Payload: &nodesandboxv1.UploadArchiveRequest_Open{Open: &nodesandboxv1.UploadArchiveOpen{
			AllocationID: "alloc-123", Attempt: 1, ExecutionLeaseToken: "lease-token", Path: "/workspace",
			Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
			SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
		}}},
		{Payload: &nodesandboxv1.UploadArchiveRequest_Chunk{Chunk: []byte("archive")}},
	}}
	if err := server.UploadArchive(upload); err != nil {
		t.Fatalf("UploadArchive() error = %v", err)
	}
	assertExecutionLeaseAccepted(t, upload.header)
	if upload.closed == nil || len(fakeService.uploadArchiveRequests) != 1 {
		t.Fatalf("upload result = %#v requests = %#v", upload.closed, fakeService.uploadArchiveRequests)
	}

	download := &fakeNodeSandboxDownloadArchiveStream{}
	if err := server.DownloadArchive(&nodesandboxv1.DownloadArchiveRequest{
		AllocationID: "alloc-123", Attempt: 1, ExecutionLeaseToken: "lease-token", Path: "/workspace",
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, download); err != nil {
		t.Fatalf("DownloadArchive() error = %v", err)
	}
	assertExecutionLeaseAccepted(t, download.header)
	if len(download.sent) != 1 || string(download.sent[0].GetChunk()) != "archive" {
		t.Fatalf("download responses = %#v", download.sent)
	}
}

func TestNodeSandboxProcessImageAcknowledgesLease(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{processImageFunc: func(stream service.ProcessImageStreamServer) error {
		open, err := stream.Recv()
		if err != nil {
			return err
		}
		if open.GetOpen().GetID() != "alloc-123" {
			t.Fatalf("image process open = %#v", open.GetOpen())
		}
		return stream.Send(&runtimev1.ProcessImageResponse{Payload: &runtimev1.ProcessImageResponse_Ready{Ready: &runtimev1.ProcessReady{}}})
	}}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())
	stream := &fakeNodeSandboxProcessImageStream{requests: []*nodesandboxv1.ProcessImageRequest{{
		Payload: &nodesandboxv1.ProcessImageRequest_Open{Open: &nodesandboxv1.ProcessImageOpen{
			AllocationID: "alloc-123", Attempt: 1, ExecutionLeaseToken: "lease-token",
			Spec: &nodesandboxv1.ImageProcessSpec{Image: "example.invalid/runtime@sha256:abc", Argv: []string{"true"}},
		}},
	}}}
	if err := server.ProcessImage(stream); err != nil {
		t.Fatalf("ProcessImage() error = %v", err)
	}
	assertExecutionLeaseAccepted(t, stream.header)
	if len(stream.sent) != 1 || stream.sent[0].GetReady() == nil {
		t.Fatalf("image process responses = %#v", stream.sent)
	}
}

func assertExecutionLeaseAccepted(t *testing.T, header metadata.MD) {
	t.Helper()
	if got := header.Get(executionLeaseAcceptedHeaderKey); len(got) != 1 || got[0] != "1" {
		t.Fatalf("execution lease acceptance header = %#v, want 1", got)
	}
}

func TestNodeSandboxWaitReportsExit(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())

	resp, err := server.WaitSandbox(context.Background(), &nodesandboxv1.WaitSandboxRequest{
		AllocationID:        "alloc-123",
		Attempt:             2,
		ExecutionLeaseToken: "lease-token",
	})
	if err != nil {
		t.Fatalf("WaitSandbox() error = %v", err)
	}
	if resp.GetState() != nodesandboxv1.SandboxProcessState_SANDBOX_PROCESS_STATE_EXITED {
		t.Fatalf("wait state = %v, want EXITED", resp.GetState())
	}
	if !resp.GetExitCodeKnown() || resp.GetExitCode() != 17 {
		t.Fatalf("wait response = %#v, want exit_code=17 known=true", resp)
	}
	if fakeService.reportedAllocationID != "alloc-123" || fakeService.reportedAttempt != 2 || fakeService.reportedExitCode != 17 || !fakeService.reportedKnown {
		t.Fatalf("reported status = allocation=%q attempt=%d exit=%d known=%v", fakeService.reportedAllocationID, fakeService.reportedAttempt, fakeService.reportedExitCode, fakeService.reportedKnown)
	}
}

func TestNodeSandboxFileMetadataBridgesRequests(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())

	statResp, err := server.StatFile(context.Background(), &nodesandboxv1.StatFileRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/out.txt",
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

	listResp, err := server.ListDir(context.Background(), &nodesandboxv1.ListDirRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp",
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

	readResp, err := server.ReadFile(context.Background(), &nodesandboxv1.ReadFileRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/out.txt",
	})
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(readResp.GetData()) != "hello" || len(fakeService.readFileRequests) != 1 || fakeService.readFileRequests[0].GetPath() != "/tmp/out.txt" {
		t.Fatalf("read response/request = response=%q requests=%#v", string(readResp.GetData()), fakeService.readFileRequests)
	}

	_, err = server.WriteFile(context.Background(), &nodesandboxv1.WriteFileRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/out.txt",
		Data:                []byte("hello"),
		CreateParents:       true,
	})
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if len(fakeService.writeFileRequests) != 1 || string(fakeService.writeFileRequests[0].GetData()) != "hello" || !fakeService.writeFileRequests[0].GetCreateParents() {
		t.Fatalf("write request = %#v", fakeService.writeFileRequests)
	}

	_, err = server.Mkdir(context.Background(), &nodesandboxv1.MkdirRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/nested",
		Parents:             true,
	})
	if err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if len(fakeService.mkdirRequests) != 1 || !fakeService.mkdirRequests[0].GetParents() {
		t.Fatalf("mkdir request = %#v", fakeService.mkdirRequests)
	}

	_, err = server.Remove(context.Background(), &nodesandboxv1.RemoveRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/nested",
		Recursive:           true,
		Force:               true,
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(fakeService.removeRequests) != 1 || !fakeService.removeRequests[0].GetRecursive() || !fakeService.removeRequests[0].GetForce() {
		t.Fatalf("remove request = %#v", fakeService.removeRequests)
	}

	existsResp, err := server.Exists(context.Background(), &nodesandboxv1.ExistsRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/out.txt",
	})
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !existsResp.GetExists() || len(fakeService.existsRequests) != 1 {
		t.Fatalf("exists response/request = response=%v requests=%#v", existsResp.GetExists(), fakeService.existsRequests)
	}

	_, err = server.Copy(context.Background(), &nodesandboxv1.CopyRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		SrcPath:             "/tmp/out.txt",
		DstPath:             "/tmp/copy.txt",
		Recursive:           true,
		Overwrite:           true,
	})
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if len(fakeService.copyRequests) != 1 || fakeService.copyRequests[0].GetDstPath() != "/tmp/copy.txt" {
		t.Fatalf("copy request = %#v", fakeService.copyRequests)
	}

	_, err = server.Move(context.Background(), &nodesandboxv1.MoveRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		SrcPath:             "/tmp/copy.txt",
		DstPath:             "/tmp/moved.txt",
		Overwrite:           true,
	})
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if len(fakeService.moveRequests) != 1 || fakeService.moveRequests[0].GetDstPath() != "/tmp/moved.txt" {
		t.Fatalf("move request = %#v", fakeService.moveRequests)
	}

	_, err = server.Chmod(context.Background(), &nodesandboxv1.ChmodRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/moved.txt",
		Mode:                0600,
		Recursive:           true,
	})
	if err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if len(fakeService.chmodRequests) != 1 || fakeService.chmodRequests[0].GetMode() != 0600 {
		t.Fatalf("chmod request = %#v", fakeService.chmodRequests)
	}

	_, err = server.Touch(context.Background(), &nodesandboxv1.TouchRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/moved.txt",
		Create:              true,
		MtimeNs:             7,
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
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())

	uploadStream := &fakeNodeSandboxUploadArchiveStream{requests: []*nodesandboxv1.UploadArchiveRequest{
		{
			Payload: &nodesandboxv1.UploadArchiveRequest_Open{Open: &nodesandboxv1.UploadArchiveOpen{
				AllocationID:        "alloc-123",
				Attempt:             1,
				ExecutionLeaseToken: "lease-token",
				Path:                "/tmp/tree",
				Format:              filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
				CreateParents:       true,
				Overwrite:           true,
				SymlinkPolicy:       filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
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

	downloadStream := &fakeNodeSandboxDownloadArchiveStream{}
	err := server.DownloadArchive(&nodesandboxv1.DownloadArchiveRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Path:                "/tmp/tree",
		Format:              filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy:       filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
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
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())

	resp, err := server.CapabilityStatus(context.Background(), &nodesandboxv1.CapabilityStatusRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
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
	if got := resp.GetCapabilities(); len(got) != 5 || got[4] != "browser" {
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
	if provider.GetName() != "browser" || provider.GetState() != "degraded" || !provider.GetAvailable() || provider.GetBackend() != "chromium" {
		t.Fatalf("provider = %#v", provider)
	}
	if len(provider.GetDependencies()) != 1 || provider.GetDependencies()[0].GetName() != "chromium" {
		t.Fatalf("provider dependencies = %#v", provider.GetDependencies())
	}
}

func TestNodeSandboxComputerUseBridgesRequests(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())
	statusResp, err := server.ComputerUseStatus(context.Background(), &nodesandboxv1.ComputerUseStatusRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
	})
	if err != nil {
		t.Fatalf("ComputerUseStatus() error = %v", err)
	}
	if !statusResp.GetAvailable() || statusResp.GetDisplay() != ":99" || statusResp.GetBackend() != "x11" {
		t.Fatalf("status response = %#v", statusResp)
	}
	screenResp, err := server.ComputerUseScreenshot(context.Background(), &nodesandboxv1.ComputerUseScreenshotRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Region:              &nodesandboxv1.ComputerUseRegion{X: 1, Y: 2, Width: 3, Height: 4},
		Format:              "jpeg",
		Quality:             75,
		Scale:               0.5,
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
	displayResp, err := server.ComputerUseDisplay(context.Background(), &nodesandboxv1.ComputerUseDisplayRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
	})
	if err != nil {
		t.Fatalf("ComputerUseDisplay() error = %v", err)
	}
	if displayResp.GetWidth() != 1280 || displayResp.GetHeight() != 720 {
		t.Fatalf("display response = %#v", displayResp)
	}
	if _, err := server.ComputerUseMouse(context.Background(), &nodesandboxv1.ComputerUseMouseRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Action:              "click",
		X:                   7,
		Y:                   9,
		Button:              "1",
	}); err != nil {
		t.Fatalf("ComputerUseMouse() error = %v", err)
	}
	if _, err := server.ComputerUseKeyboard(context.Background(), &nodesandboxv1.ComputerUseKeyboardRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Text:                "hello",
	}); err != nil {
		t.Fatalf("ComputerUseKeyboard() error = %v", err)
	}
	if len(fakeService.computerUseDisplayReqs) != 1 || len(fakeService.computerUseMouseReqs) != 1 || len(fakeService.computerUseKeyboardReqs) != 1 {
		t.Fatalf("computer-use requests display=%d mouse=%d keyboard=%d", len(fakeService.computerUseDisplayReqs), len(fakeService.computerUseMouseReqs), len(fakeService.computerUseKeyboardReqs))
	}
}

func TestNodeSandboxBrowserBridgesRequests(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())

	statusResp, err := server.BrowserStatus(context.Background(), &nodesandboxv1.BrowserStatusRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
	})
	if err != nil {
		t.Fatalf("BrowserStatus() error = %v", err)
	}
	if !statusResp.GetAvailable() || statusResp.GetCommand() != "chromium" || statusResp.GetRunning() {
		t.Fatalf("status response = %#v", statusResp)
	}

	openResp, err := server.BrowserOpen(context.Background(), &nodesandboxv1.BrowserOpenRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Url:                 "data:text/html,open",
	})
	if err != nil {
		t.Fatalf("BrowserOpen() error = %v", err)
	}
	if !openResp.GetRunning() || openResp.GetPid() != 99 || openResp.GetUrl() != "data:text/html,open" {
		t.Fatalf("open response = %#v", openResp)
	}

	if _, err := server.BrowserNavigate(context.Background(), &nodesandboxv1.BrowserNavigateRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Url:                 "data:text/html,navigate",
	}); err != nil {
		t.Fatalf("BrowserNavigate() error = %v", err)
	}
	if _, err := server.BrowserResize(context.Background(), &nodesandboxv1.BrowserResizeRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Width:               1024,
		Height:              768,
	}); err != nil {
		t.Fatalf("BrowserResize() error = %v", err)
	}
	if _, err := server.BrowserClick(context.Background(), &nodesandboxv1.BrowserClickRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		X:                   7,
		Y:                   9,
		Button:              "left",
	}); err != nil {
		t.Fatalf("BrowserClick() error = %v", err)
	}
	if _, err := server.BrowserType(context.Background(), &nodesandboxv1.BrowserTypeRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		Text:                "hello",
		DelayMs:             5,
	}); err != nil {
		t.Fatalf("BrowserType() error = %v", err)
	}
	if _, err := server.BrowserWait(context.Background(), &nodesandboxv1.BrowserWaitRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
		TimeoutMs:           250,
	}); err != nil {
		t.Fatalf("BrowserWait() error = %v", err)
	}
	if _, err := server.BrowserClose(context.Background(), &nodesandboxv1.BrowserCloseRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: "lease-token",
	}); err != nil {
		t.Fatalf("BrowserClose() error = %v", err)
	}

	if len(fakeService.browserStatusReqs) != 1 || fakeService.browserStatusReqs[0].GetID() != "alloc-123" {
		t.Fatalf("browser status requests = %#v", fakeService.browserStatusReqs)
	}
	if len(fakeService.browserCloseReqs) != 1 || fakeService.browserCloseReqs[0].GetID() != "alloc-123" {
		t.Fatalf("browser close requests = %#v", fakeService.browserCloseReqs)
	}
	if len(fakeService.browserOpenReqs) != 1 || fakeService.browserOpenReqs[0].GetUrl() != "data:text/html,open" {
		t.Fatalf("browser open requests = %#v", fakeService.browserOpenReqs)
	}
	if len(fakeService.browserNavigateReqs) != 1 || fakeService.browserNavigateReqs[0].GetUrl() != "data:text/html,navigate" {
		t.Fatalf("browser navigate requests = %#v", fakeService.browserNavigateReqs)
	}
	if len(fakeService.browserResizeReqs) != 1 || fakeService.browserResizeReqs[0].GetWidth() != 1024 || fakeService.browserResizeReqs[0].GetHeight() != 768 {
		t.Fatalf("browser resize requests = %#v", fakeService.browserResizeReqs)
	}
	if len(fakeService.browserClickReqs) != 1 || fakeService.browserClickReqs[0].GetButton() != "left" {
		t.Fatalf("browser click requests = %#v", fakeService.browserClickReqs)
	}
	if len(fakeService.browserTypeReqs) != 1 || fakeService.browserTypeReqs[0].GetText() != "hello" || fakeService.browserTypeReqs[0].GetDelayMs() != 5 {
		t.Fatalf("browser type requests = %#v", fakeService.browserTypeReqs)
	}
	if len(fakeService.browserWaitReqs) != 1 || fakeService.browserWaitReqs[0].GetTimeoutMs() != 250 {
		t.Fatalf("browser wait requests = %#v", fakeService.browserWaitReqs)
	}
}

func TestNodeSandboxUploadArchiveRequiresOpenFrame(t *testing.T) {
	t.Parallel()

	server := NewNodeSandboxServer(&fakeNodeSandboxService{}, "node-a", NewAllocationTargetRegistry())
	err := server.UploadArchive(&fakeNodeSandboxUploadArchiveStream{requests: []*nodesandboxv1.UploadArchiveRequest{
		{Payload: &nodesandboxv1.UploadArchiveRequest_Chunk{Chunk: []byte("archive")}},
	}})

	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("UploadArchive() code = %v, want InvalidArgument", grpcstatus.Code(err))
	}
}

func TestNodeSandboxUploadArchiveRequiresNonEmptyStream(t *testing.T) {
	t.Parallel()

	server := NewNodeSandboxServer(&fakeNodeSandboxService{}, "node-a", NewAllocationTargetRegistry())
	err := server.UploadArchive(&fakeNodeSandboxUploadArchiveStream{})

	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("UploadArchive() code = %v, want InvalidArgument", grpcstatus.Code(err))
	}
}

func TestNodeSandboxUploadArchiveDoesNotAcknowledgeRejectedLease(t *testing.T) {
	t.Parallel()

	server := NewNodeSandboxServer(&fakeNodeSandboxService{}, "node-a", NewAllocationTargetRegistry())
	stream := &fakeNodeSandboxUploadArchiveStream{requests: []*nodesandboxv1.UploadArchiveRequest{
		{Payload: &nodesandboxv1.UploadArchiveRequest_Open{Open: &nodesandboxv1.UploadArchiveOpen{
			AllocationID: "alloc-123", Attempt: 1, Path: "/workspace",
			Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
			SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
		}}},
	}}
	err := server.UploadArchive(stream)

	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("UploadArchive() code = %v, want Unauthenticated", grpcstatus.Code(err))
	}
	if got := stream.header.Get(executionLeaseAcceptedHeaderKey); len(got) != 0 {
		t.Fatalf("rejected lease acceptance header = %#v, want none", got)
	}
}

func TestNodeSandboxExecRequiresAllocationLease(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry())

	_, err := server.Exec(context.Background(), &nodesandboxv1.ExecRequest{
		AllocationID: "alloc-123",
		Attempt:      1,
		Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"true"}},
	})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Exec() error code = %v, want %v", grpcstatus.Code(err), codes.Unauthenticated)
	}
}

func TestNodeSandboxExecAcceptsLeaseCacheTokenHash(t *testing.T) {
	t.Parallel()

	token := "lease-token"
	sum := sha256.Sum256([]byte(token))
	cache := controlplane.NewLeaseCache()
	cache.Apply([]*commonv1.ExecutionLease{{
		LeaseID:             "lease-123",
		AllocationID:        "alloc-123",
		Attempt:             1,
		ValidationTokenHash: hex.EncodeToString(sum[:]),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
	}})
	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry(), cache)

	_, err := server.Exec(context.Background(), &nodesandboxv1.ExecRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: token,
		Spec:                &nodesandboxv1.ExecSpec{Argv: []string{"true"}},
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
}

func TestNodeSandboxExecRejectsRevokedLeaseFromCache(t *testing.T) {
	t.Parallel()

	token := "lease-token"
	sum := sha256.Sum256([]byte(token))
	cache := controlplane.NewLeaseCache()
	cache.Apply([]*commonv1.ExecutionLease{{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ValidationTokenHash: hex.EncodeToString(sum[:]),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
		Revoked:             true,
	}})
	fakeService := &fakeNodeSandboxService{}
	server := NewNodeSandboxServer(fakeService, "node-a", NewAllocationTargetRegistry(), cache)

	_, err := server.Exec(context.Background(), &nodesandboxv1.ExecRequest{
		AllocationID:        "alloc-123",
		Attempt:             1,
		ExecutionLeaseToken: token,
		Spec:                &nodesandboxv1.ExecSpec{Argv: []string{"true"}},
	})
	if grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("Exec() error code = %v, want %v", grpcstatus.Code(err), codes.Unauthenticated)
	}
}
