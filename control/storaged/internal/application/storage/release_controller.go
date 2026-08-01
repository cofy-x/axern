package storage

import (
	"context"
	"fmt"
	"strings"

	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

// ReleaseWorkloadVolumeClaims clears the owner of every live claim owned by
// the deleting workload so the backend data stays re-attachable by a future
// workload through the ownerless claim path in ensureClaimAndClass.
func (c *Controller) ReleaseWorkloadVolumeClaims(ctx context.Context, req *privatestoragev1.ReleaseWorkloadVolumeClaimsRequest) (*privatestoragev1.ReleaseWorkloadVolumeClaimsResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	workloadID := strings.TrimSpace(req.GetWorkloadID())
	workloadType := strings.TrimSpace(req.GetWorkloadType())
	if namespace == "" || workloadID == "" || workloadType == "" {
		return nil, fmt.Errorf("storage release workload namespace, workload id, and workload type are required")
	}
	releasedClaimIDs, err := c.store.ReleaseWorkloadVolumeClaims(ctx, namespace, workloadID, workloadType, c.now().UTC())
	if err != nil {
		return nil, err
	}
	return &privatestoragev1.ReleaseWorkloadVolumeClaimsResponse{ReleasedClaimIds: releasedClaimIDs}, nil
}
