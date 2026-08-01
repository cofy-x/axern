package storage

import (
	"context"
	"time"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

type Store interface {
	CreateVolumeClass(context.Context, *storagev1.VolumeClass) (*storagev1.VolumeClass, error)
	GetVolumeClass(context.Context, string) (*storagev1.VolumeClass, bool, error)
	ListVolumeClasses(context.Context) ([]*storagev1.VolumeClass, error)
	CreateVolumeClaim(context.Context, *storagev1.VolumeClaim) (*storagev1.VolumeClaim, error)
	GetVolumeClaim(context.Context, string, string) (*storagev1.VolumeClaim, bool, error)
	ListVolumeClaims(context.Context, *storagev1.VolumeClaimListFilter) ([]*storagev1.VolumeClaim, error)
	UpdateVolumeClaim(context.Context, string, string, int64, func(*storagev1.VolumeClaim) error) (*storagev1.VolumeClaim, error)
	ReleaseWorkloadVolumeClaims(context.Context, string, string, string, time.Time) ([]string, error)
	GetVolumeBindingForClaim(context.Context, string) (*privatestoragev1.VolumeBinding, bool, error)
	ReserveVolumeBinding(context.Context, kernel.VolumeBindingReserve) (*privatestoragev1.ResolvedNodeVolume, error)
	ReleaseVolumeBindings(context.Context, string, string) error
	RetryFailedVolumeBinding(context.Context, string, string) (*privatestoragev1.VolumeBinding, error)
	ListVolumeBindings(context.Context, kernel.VolumeBindingListFilter) ([]*privatestoragev1.VolumeBinding, error)
	ReportVolumePublish(context.Context, string, string, []*privatestoragev1.VolumePublishObservation) error
	ReportVolumeRelease(context.Context, string, string, []*privatestoragev1.VolumeReleaseObservation) error
	GetVolumeBindingHealth(context.Context, time.Duration) (*privatestoragev1.VolumeBindingHealth, error)
	ClaimVolumeReclaims(context.Context, *privatestoragev1.ClaimVolumeReclaimsRequest) ([]*privatestoragev1.VolumeReclaim, error)
	ReportVolumeReclaim(context.Context, *privatestoragev1.ReportVolumeReclaimRequest) error
	GetVolumeReclaimQueueHealth(context.Context) (*privatestoragev1.VolumeReclaimQueueHealth, error)
}

type Controller struct {
	store Store
	now   func() time.Time
}

func NewController(store Store) *Controller {
	return &Controller{store: store, now: time.Now}
}
