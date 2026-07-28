package parse

import (
	"fmt"
	"slices"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

const (
	validServiceReplicaViews = "all, current, ended, unhealthy, outdated, updated"
	validServiceStatuses     = "reconciling, ready, degraded, failed, deleting, deleted"
)

func ServiceVolumeMounts(values []string) ([]*commonv1.ServiceVolumeMount, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*commonv1.ServiceVolumeMount, 0, len(values))
	for _, value := range values {
		parts := strings.SplitN(value, ":", 3)
		if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid volume %q, want name:/container/path[:options]", value)
		}
		mount := &commonv1.ServiceVolumeMount{
			Name:   strings.TrimSpace(parts[0]),
			Target: strings.TrimSpace(parts[1]),
		}
		explicitRW := false
		if len(parts) == 3 {
			for _, option := range splitList([]string{parts[2]}) {
				switch option {
				case "ro":
					if explicitRW {
						return nil, fmt.Errorf("invalid volume %q, ro and rw cannot be combined", value)
					}
					mount.Readonly = true
				case "rw":
					if mount.Readonly {
						return nil, fmt.Errorf("invalid volume %q, ro and rw cannot be combined", value)
					}
					explicitRW = true
				default:
					if !slices.Contains(mount.Options, option) {
						mount.Options = append(mount.Options, option)
					}
				}
			}
		}
		out = append(out, mount)
	}
	return out, nil
}

func ImageMounts(values []string) ([]*commonv1.ImageMount, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*commonv1.ImageMount, 0, len(values))
	for _, value := range values {
		image, targetAndOptions, ok := strings.Cut(value, ":/")
		if !ok || strings.TrimSpace(image) == "" || strings.TrimSpace(targetAndOptions) == "" {
			return nil, fmt.Errorf("invalid image mount %q, want image:/container/path[:ro]", value)
		}
		target := "/" + targetAndOptions
		options := ""
		if idx := strings.LastIndex(target, ":"); idx >= 0 {
			options = strings.TrimSpace(target[idx+1:])
			target = target[:idx]
		}
		if options != "" && options != "ro" {
			return nil, fmt.Errorf("invalid image mount %q, only ro option is supported", value)
		}
		out = append(out, &commonv1.ImageMount{
			Image:    strings.TrimSpace(image),
			Target:   strings.TrimSpace(target),
			Readonly: true,
		})
	}
	return out, nil
}

func ServiceReplicaView(value string) (servicev1.ServiceReplicaView, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ALL, nil
	case "current":
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT, nil
	case "ended":
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ENDED, nil
	case "unhealthy":
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UNHEALTHY, nil
	case "outdated":
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_OUTDATED, nil
	case "updated":
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UPDATED, nil
	default:
		return servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UNSPECIFIED, fmt.Errorf("invalid service replica view %q, want one of: %s", value, validServiceReplicaViews)
	}
}

func ServiceStatuses(values []string) ([]servicev1.ServiceStatus, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]servicev1.ServiceStatus, 0, len(values))
	for _, value := range splitList(values) {
		switch normalizeToken(value) {
		case "reconciling":
			out = append(out, servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING)
		case "ready":
			out = append(out, servicev1.ServiceStatus_SERVICE_STATUS_READY)
		case "degraded":
			out = append(out, servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED)
		case "failed":
			out = append(out, servicev1.ServiceStatus_SERVICE_STATUS_FAILED)
		case "deleting":
			out = append(out, servicev1.ServiceStatus_SERVICE_STATUS_DELETING)
		case "deleted":
			out = append(out, servicev1.ServiceStatus_SERVICE_STATUS_DELETED)
		default:
			return nil, fmt.Errorf("invalid service status %q, want one of: %s", value, validServiceStatuses)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
