package servicekernel

import (
	"context"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type Store interface {
	Reader
	Mutator
}

type Reader interface {
	ReplicaReader
	EventReader
	Get(ctx context.Context, id string) (*servicev1.Service, bool, error)
	List(ctx context.Context, filter *servicev1.ServiceListFilter) ([]*servicev1.Service, error)
}

type Watcher interface {
	Watch(ctx context.Context, serviceID string, afterVersion int64) (WatchStream, error)
}

type WatchStream interface {
	Next(ctx context.Context) (*servicev1.Service, error)
	Close()
}

type AutoscalingSweepReader interface {
	ListAutoscaled(ctx context.Context) ([]*servicev1.Service, error)
}

type Mutator interface {
	Create(ctx context.Context, params CreateParams, now time.Time) (*servicev1.Service, error)
	Update(ctx context.Context, req *servicev1.UpdateServiceRequest, now time.Time) (*servicev1.Service, error)
	Delete(ctx context.Context, params DeleteParams, now time.Time) (*servicev1.Service, bool, error)
	Purge(ctx context.Context, id string, now time.Time) (string, bool, error)
}

type ReplicaReader interface {
	GetReplica(ctx context.Context, serviceID, replicaID string) (*servicev1.ServiceReplica, bool, error)
	ListReplicas(ctx context.Context, serviceID string, filter *servicev1.ServiceReplicaListFilter) ([]*servicev1.ServiceReplica, error)
}

type EventReader interface {
	ListEvents(ctx context.Context, serviceID string, limit int32) ([]*servicev1.ServiceEvent, error)
}

type EventWriter interface {
	RecordEvent(ctx context.Context, event *servicev1.ServiceEvent) error
}

type StatusStore interface {
	UpdateStatus(ctx context.Context, serviceID string, status servicev1.ServiceStatus, message string, now time.Time) (*servicev1.Service, error)
	UpdateAutoscalingStatus(ctx context.Context, serviceID string, autoscaling *servicev1.ServiceAutoscalingStatus, now time.Time) (*servicev1.Service, error)
	SyncObservedStatus(ctx context.Context, serviceID string, now time.Time) (*servicev1.Service, error)
	UpdateDeletionStatus(ctx context.Context, serviceID string, deletion *servicev1.ServiceDeletionStatus, now time.Time) (*servicev1.Service, error)
}

type Reconciler interface {
	ReconcilePending(ctx context.Context, now time.Time) error
	ReconcileAutoscaled(ctx context.Context, now time.Time) error
	ReconcileServices(ctx context.Context, serviceIDs []string, now time.Time) error
}

type AllocationReconciler interface {
	ReconcileAllocationBatch(ctx context.Context, now time.Time) (int, error)
}

type Controller interface {
	Store
	Reconciler
	AllocationReconciler
	VolumeReclaimDispatcher
}

type CreateParams struct {
	Namespace      string
	EnvironmentID  string
	Replicas       int32
	Config         *commonv1.ExecutionConfig
	Labels         map[string]string
	RolloutPolicy  *servicev1.ServiceRolloutPolicy
	ReadinessProbe *servicev1.ServiceProbe
	LivenessProbe  *servicev1.ServiceProbe
	Autoscaling    *servicev1.ServiceAutoscalingPolicy
}

type DeleteParams struct {
	ServiceID         string
	ExpectedVersion   int64
	RequireSuspended  bool
	VolumeDisposition servicev1.ServiceVolumeDisposition
}
