package service

import (
	"context"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	"io"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type SandboxService interface {
	ReadAllocationOutput(context.Context, string, string) ([]allocationoutput.Chunk, bool, error)
	SealedOutputManifest(context.Context, string) (allocationoutput.Manifest, error)
	ReadSealedOutput(context.Context, string, string, int64, int64) ([]byte, int64, bool, error)
	// Sandbox-local data-plane operations.
	SandboxFileService
	SandboxComputerUseService
	SandboxCapabilityService

	// Sandbox lifecycle and process execution.
	Start(context.Context, *runtime.StartRequest) (*runtime.StartResponse, error)
	Delete(context.Context, *runtime.DeleteRequest) (*runtime.DeleteResponse, error)
	Exec(context.Context, *runtime.ExecRequest) (*runtime.ExecResponse, error)
	ExecStream(ExecStreamServer) error
	Process(ProcessStreamServer) error
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
	IsControlPlaneAllocation(string) bool
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

type SandboxCapabilityService interface {
	SandboxCapabilityStatus(ctx context.Context, containerID string) (SandboxCapabilityStatus, error)
}

type NodeOperatorService interface {
	List(context.Context, *runtime.ListContainersRequest) (*runtime.ListContainersResponse, error)
	Exec(context.Context, *runtime.ExecRequest) (*runtime.ExecResponse, error)
	ExecStream(ExecStreamServer) error
	Wait(context.Context, *runtime.WaitRequest) (*runtime.WaitResponse, error)
	NodeInventory() (nodeinventory.NodeInventorySnapshot, bool)
	SandboxdDiagnostics(ctx context.Context, containerID string, full bool) (SandboxdDiagnostics, error)
	NetworkPolicyDiagnostics(context.Context, string) NetworkPolicyDiagnostics
	ValidateOperatorInspection(string) error
	ValidateOperatorExecution(string) error
	ForceTerminateAllocation(context.Context, string, string) error
	ForceCleanupAllocation(context.Context, string, string, int64) error
}

type AllocationNetworkService interface {
	ResolveAllocationNetwork(string) (*SandboxNetwork, error)
}

type NodeLifecycleService interface {
	Start(context.Context, *runtime.StartRequest) (*runtime.StartResponse, error)
	Delete(context.Context, *runtime.DeleteRequest) (*runtime.DeleteResponse, error)
	StartControlPlaneAllocation(context.Context, string, *runtime.StartRequest) (*runtime.StartResponse, error)
	DeleteControlPlaneAllocation(context.Context, string, *runtime.DeleteRequest) (*runtime.DeleteResponse, error)
	HasControlPlaneAllocation(string, string) bool
	IsControlPlaneAllocation(string) bool
	List(context.Context, *runtime.ListContainersRequest) (*runtime.ListContainersResponse, error)
	ReconcileAllocationCapabilities(context.Context, string) ([]*capabilityv1.CapabilityRequirement, *capabilityv1.CapabilityConditionSet, error)
}

// NodeService is the daemon's assembled service surface. Protocol servers
// receive only the narrower interface they own.
type NodeService interface {
	SandboxService
	NodeOperatorService
	NodeLifecycleService
	AllocationNetworkService
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
