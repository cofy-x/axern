package contract

import (
	"context"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
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

type ExecutionEnvelope struct {
	ContainerID string
	BundlePath  string
	Metadata    *apipb.ContainerMetadata
}

type ExecutionEnvelopeHandler interface {
	EligibleForExecutionEnvelope(*apipb.StartRequest) bool
	PrepareExecutionEnvelope(context.Context, *apipb.CreateContainerRequest, HandlerOptions) (*ExecutionEnvelope, error)
	ActivateExecutionEnvelope(context.Context, *ExecutionEnvelope, HandlerOptions) (*apipb.ContainerMetadata, error)
}

// PersistentStorageReconciler converges runtime-private storage only after the
// caller has restored a complete allocation view. Implementations must retain
// artifacts when runtime inventory is unavailable or ownership is ambiguous.
type PersistentStorageReconciler interface {
	ReconcilePersistentStorage(context.Context, []string) error
}

type RuntimeHandler interface {
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
