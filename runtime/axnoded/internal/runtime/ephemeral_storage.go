package runtime

import (
	"fmt"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"google.golang.org/protobuf/proto"
)

func resolveEphemeralStorage(request *apipb.CreateContainerRequest, defaultLimit int64) (*apipb.CreateContainerRequest, error) {
	if request == nil || request.GetRootfs() == nil {
		return nil, fmt.Errorf("container rootfs is required")
	}
	requested := request.GetEphemeralStorageRequestBytes()
	limit := request.GetEphemeralStorageLimitBytes()
	if requested < 0 || limit < 0 {
		return nil, fmt.Errorf("ephemeral storage request and limit must be >= 0: request=%d limit=%d", requested, limit)
	}
	if request.GetRootfs().GetReadonly() {
		if requested != 0 || limit != 0 {
			return nil, fmt.Errorf("readonly rootfs conflicts with ephemeral storage resources: request=%d limit=%d", requested, limit)
		}
		return request, nil
	}
	if limit == 0 {
		limit = defaultLimit
	}
	if limit <= 0 {
		return nil, fmt.Errorf("writable rootfs requires ephemeral storage limit > 0")
	}
	if requested == 0 {
		requested = limit
	}
	if requested > limit {
		return nil, fmt.Errorf("ephemeral storage request must be <= limit: request=%d limit=%d", requested, limit)
	}
	cloned := proto.Clone(request).(*apipb.CreateContainerRequest)
	cloned.EphemeralStorageRequestBytes = requested
	cloned.EphemeralStorageLimitBytes = limit
	return cloned, nil
}
