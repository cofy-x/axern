package service

import (
	"context"
	"io"
	"net/http"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type SandboxService interface {
	// Sandbox-local data-plane operations.
	SandboxFileService
	SandboxComputerUseService
	SandboxBrowserService
	SandboxCapabilityService

	// Sandbox lifecycle and process execution.
	Start(context.Context, *runtime.StartRequest) (*runtime.StartResponse, error)
	Delete(context.Context, *runtime.DeleteRequest) (*runtime.DeleteResponse, error)
	Exec(context.Context, *runtime.ExecRequest) (*runtime.ExecResponse, error)
	ExecStream(ExecStreamServer) error
	Process(ProcessStreamServer) error
	ProxyHTTP(HTTPProxyServer) error
	Wait(context.Context, *runtime.WaitRequest) (*runtime.WaitResponse, error)

	// Sandbox inspection and control.
	List(context.Context, *runtime.ListContainersRequest) (*runtime.ListContainersResponse, error)
	Stats(context.Context, *runtime.StatsRequest) (*runtime.StatsResponse, error)
	Kill(context.Context, *runtime.KillRequest) (*runtime.KillResponse, error)
	Version(context.Context, *runtime.VersionRequest) (*runtime.VersionResponse, error)

	// Node daemon lifecycle and status reporting.
	Run(context.Context) error
	Shutdown(context.Context) error
	Ready() bool
	ReportAllocationLifecycle(allocationID string, status commonv1.AllocationLifecycleState, exitCode *int32, ready bool, readinessMessage string, message string, observedAt time.Time)
	NodeInventory() (nodeinventory.NodeInventorySnapshot, bool)
}

// ControlPlaneAllocationService owns the durable admission relationship
// between a controld node binding and one allocation. It is deliberately
// separate from SandboxService: node-local sandbox operations must not be
// able to create control-plane authority implicitly.
type ControlPlaneAllocationService interface {
	StartControlPlaneAllocation(context.Context, string, *runtime.StartRequest) (*runtime.StartResponse, error)
	DeleteControlPlaneAllocation(context.Context, string, *runtime.DeleteRequest) (*runtime.DeleteResponse, error)
	HasControlPlaneAllocation(string, string) bool
}

type SandboxFileService interface {
	StatFile(context.Context, *runtime.StatFileRequest) (*runtime.StatFileResponse, error)
	ListDir(context.Context, *runtime.ListDirRequest) (*runtime.ListDirResponse, error)
	ReadFile(context.Context, *runtime.ReadFileRequest) (*runtime.ReadFileResponse, error)
	WriteFile(context.Context, *runtime.WriteFileRequest) (*runtime.WriteFileResponse, error)
	Mkdir(context.Context, *runtime.MkdirRequest) (*runtime.MkdirResponse, error)
	Remove(context.Context, *runtime.RemoveRequest) (*runtime.RemoveResponse, error)
	Exists(context.Context, *runtime.ExistsRequest) (*runtime.ExistsResponse, error)
	Copy(context.Context, *runtime.CopyRequest) (*runtime.CopyResponse, error)
	Move(context.Context, *runtime.MoveRequest) (*runtime.MoveResponse, error)
	Chmod(context.Context, *runtime.ChmodRequest) (*runtime.ChmodResponse, error)
	Touch(context.Context, *runtime.TouchRequest) (*runtime.TouchResponse, error)
	UploadArchive(context.Context, *runtime.UploadArchiveRequest, io.Reader) (*runtime.UploadArchiveResponse, error)
	DownloadArchive(context.Context, *runtime.DownloadArchiveRequest, io.Writer) (*runtime.DownloadArchiveResponse, error)
}

type SandboxComputerUseService interface {
	ComputerUseStatus(context.Context, *runtime.ComputerUseStatusRequest) (*runtime.ComputerUseStatusResponse, error)
	ComputerUseScreenshot(context.Context, *runtime.ComputerUseScreenshotRequest) (*runtime.ComputerUseScreenshotResponse, error)
	ComputerUseDisplay(context.Context, *runtime.ComputerUseDisplayRequest) (*runtime.ComputerUseDisplayResponse, error)
	ComputerUseMouse(context.Context, *runtime.ComputerUseMouseRequest) (*runtime.ComputerUseMouseResponse, error)
	ComputerUseKeyboard(context.Context, *runtime.ComputerUseKeyboardRequest) (*runtime.ComputerUseKeyboardResponse, error)
}

type SandboxBrowserService interface {
	BrowserStatus(context.Context, *runtime.BrowserStatusRequest) (*runtime.BrowserStatusResponse, error)
	BrowserOpen(context.Context, *runtime.BrowserOpenRequest) (*runtime.BrowserStatusResponse, error)
	BrowserClose(context.Context, *runtime.BrowserCloseRequest) (*runtime.BrowserStatusResponse, error)
	BrowserNavigate(context.Context, *runtime.BrowserNavigateRequest) (*runtime.BrowserStatusResponse, error)
	BrowserResize(context.Context, *runtime.BrowserResizeRequest) (*runtime.BrowserStatusResponse, error)
	BrowserClick(context.Context, *runtime.BrowserClickRequest) (*runtime.BrowserStatusResponse, error)
	BrowserType(context.Context, *runtime.BrowserTypeRequest) (*runtime.BrowserStatusResponse, error)
	BrowserWait(context.Context, *runtime.BrowserWaitRequest) (*runtime.BrowserStatusResponse, error)
}

type SandboxCapabilityService interface {
	SandboxCapabilityStatus(ctx context.Context, containerID string) (SandboxCapabilityStatus, error)
}

type NodeOperatorService interface {
	SandboxService
	ReconcileAllocationCapabilities(context.Context, string) ([]*capabilityv1.CapabilityRequirement, *capabilityv1.CapabilityConditionSet, error)
	NetworkForSandbox(containerID string) (*SandboxNetwork, error)
	SandboxdDiagnostics(ctx context.Context, containerID string, full bool) (SandboxdDiagnostics, error)
	NetworkPolicyDiagnostics(context.Context, string) NetworkPolicyDiagnostics
}

// NodeService is the daemon's assembled service surface. Protocol servers
// receive only the narrower interface they own.
type NodeService interface {
	NodeOperatorService
	ControlPlaneAllocationService
}

type NetworkPolicyMode string

const (
	NetworkPolicyModeUnrestricted NetworkPolicyMode = "unrestricted"
	NetworkPolicyModeDNSDeny      NetworkPolicyMode = "dns_deny"
	NetworkPolicyModeStrict       NetworkPolicyMode = "strict"
)

type NetworkPolicyStatus string

const (
	NetworkPolicyStatusOK                    NetworkPolicyStatus = "ok"
	NetworkPolicyStatusAbsent                NetworkPolicyStatus = "absent"
	NetworkPolicyStatusCapabilityUnavailable NetworkPolicyStatus = "capability_unavailable"
	NetworkPolicyStatusEnforcementUnhealthy  NetworkPolicyStatus = "enforcement_unhealthy"
	NetworkPolicyStatusBindingMismatch       NetworkPolicyStatus = "binding_mismatch"
)

type NetworkPolicyCapabilityState string

const (
	NetworkPolicyCapabilityAvailable   NetworkPolicyCapabilityState = "available"
	NetworkPolicyCapabilityUnavailable NetworkPolicyCapabilityState = "unavailable"
	NetworkPolicyCapabilityUnknown     NetworkPolicyCapabilityState = "unknown"
	NetworkPolicyCapabilityNotRequired NetworkPolicyCapabilityState = "not_required"
)

type NetworkPolicyDiagnostics struct {
	Mode                NetworkPolicyMode
	Status              NetworkPolicyStatus
	CapabilityState     NetworkPolicyCapabilityState
	EnforcementHealthy  bool
	ExactBinding        bool
	EnforcementRevision int64
	DomainRuleCount     uint32
	CIDRRuleCount       uint32
	PortRangeCount      uint32
	TotalRuleCount      uint32
}

type SandboxNetwork struct {
	IP        string
	NetNSPath string
}

type ExecStreamServer interface {
	Recv() (*runtime.ExecStreamRequest, error)
	Send(*runtime.ExecStreamResponse) error
	Context() context.Context
}

type ProcessStreamServer interface {
	Recv() (*runtime.ProcessRequest, error)
	Send(*runtime.ProcessResponse) error
	Context() context.Context
}

type HTTPProxyServer interface {
	TargetID() string
	Port() int32
	Method() string
	Path() string
	Query() string
	Header() http.Header
	HasBody() bool
	ContentLength() int64
	RecvBody() ([]byte, error)
	SendHead(statusCode int, header http.Header) error
	SendBody([]byte) error
	SendTrailers(http.Header) error
	Context() context.Context
}
