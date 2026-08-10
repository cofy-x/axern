package contract

import (
	"context"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

type RuntimeCapabilities struct {
	CanCheckpoint bool
	CanExecDirect bool
}

type RuntimeRequirements struct {
	NeedsCgroup           bool
	NeedsNetworkNamespace bool
	Resources             []resourcemanager.ResourceName
}

// PreparedContainer is an allocation-owned OCI container that has completed
// runtime create but has not started user code. Its identity and host resources
// are immutable and may not be rebound to another allocation.
type PreparedContainer struct {
	ContainerID string
	BundlePath  string
	Metadata    *apipb.ContainerMetadata
}

// ManagedRuntimeHandler is the fail-closed lifecycle contract for workload
// allocations. Managed starts must be split into OCI create and start so
// allocation-specific enforcement can be verified before user code executes.
// RuntimeHandler.CreateContainer remains available to node-owned auxiliary
// containers whose lifecycle is not an allocation lifecycle.
type ManagedRuntimeHandler interface {
	RuntimeHandler
	PrepareContainer(context.Context, *apipb.CreateContainerRequest, HandlerOptions) (*PreparedContainer, error)
	StartPreparedContainer(context.Context, *PreparedContainer, HandlerOptions) (*apipb.ContainerMetadata, error)
}

// PersistentStorageReconciler converges runtime-private storage against a
// successfully collected runtime inventory. Persisted allocation/container
// metadata is recovery input, not proof that a runtime still exists. Callers
// must not invoke destructive reconciliation unless every enabled runtime
// inventory was collected without error.
type PersistentStorageReconciler interface {
	ReconcilePersistentStorage(context.Context, map[string]struct{}) error
}

type AllocationCapabilityVerifier interface {
	VerifyAllocationCapability(context.Context, *capabilityv1.CapabilityDependency, HandlerOptions) CapabilityVerification
}

// AllocationEnforcementManifestProvider returns the immutable launch contract
// written by the runtime before the process is created.
type AllocationEnforcementManifestProvider interface {
	AllocationEnforcementManifest(context.Context, string) (*apipb.AllocationEnforcementManifest, error)
}

type CapabilityVerificationState uint8

const (
	CapabilityVerificationInconclusive CapabilityVerificationState = iota
	CapabilityVerificationVerified
	CapabilityVerificationLost
)

// CapabilityVerification separates a proven enforcement loss from an
// observation error. Callers may fail-stop immediately only for Lost; an
// Inconclusive result must follow the bounded retry policy.
type CapabilityVerification struct {
	State CapabilityVerificationState
	Err   error
}

func VerifiedCapability() CapabilityVerification {
	return CapabilityVerification{State: CapabilityVerificationVerified}
}

func LostCapability(err error) CapabilityVerification {
	return CapabilityVerification{State: CapabilityVerificationLost, Err: err}
}

func InconclusiveCapability(err error) CapabilityVerification {
	return CapabilityVerification{State: CapabilityVerificationInconclusive, Err: err}
}

type RuntimeHandler interface {
	AllocationEnforcementManifestProvider
	Name() string
	Capabilities() RuntimeCapabilities
	Requirements() RuntimeRequirements
	Version(context.Context) (*apipb.RuntimeVersion, error)
	CreateContainer(context.Context, *apipb.CreateContainerRequest, HandlerOptions) (*apipb.ContainerMetadata, error)
	DeleteContainer(context.Context, *apipb.DeleteContainerRequest, HandlerOptions) (*apipb.DeleteContainerResponse, error)
	KillContainer(context.Context, *apipb.SignalContainerRequest, HandlerOptions) (*apipb.SignalContainerResponse, error)
	ListContainers(context.Context, HandlerOptions) ([]*UnionContainerState, error)
	ContainerSpec(context.Context, HandlerOptions) (*spec.Spec, error)
	ExecContainer(context.Context, *apipb.ExecContainerRequest, HandlerOptions) (*apipb.ExecContainerResponse, error)
	OpenExecSession(context.Context, *apipb.ExecSessionOpen, HandlerOptions) (Session, error)
	ProcessService() ProcessService
	FileService() FileService
	CheckpointContainer(*apipb.CheckpointRequest) error
	Wait(context.Context, HandlerOptions) (Exit, error)
	ShutDown()
}
