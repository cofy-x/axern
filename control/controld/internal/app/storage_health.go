package app

import (
	"context"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

type nodeHealthSource struct {
	store           nodeHealthRecordSource
	heartbeatWindow time.Duration
	summaryWindow   time.Duration
}

type nodeHealthRecordSource interface {
	ListNodes(ctx context.Context, filter adminkernel.NodeListFilter) ([]*nodekernel.Record, error)
}

func (s nodeHealthSource) NodeHealth(ctx context.Context, now time.Time) (adminkernel.NodeFleetHealth, adminkernel.NodeVolumeHealth, error) {
	if s.store == nil {
		return adminkernel.NodeFleetHealth{}, adminkernel.NodeVolumeHealth{}, nil
	}
	records, err := s.store.ListNodes(ctx, adminkernel.NodeListFilter{Lifecycle: nodekernel.LifecycleActive})
	if err != nil {
		return adminkernel.NodeFleetHealth{}, adminkernel.NodeVolumeHealth{}, err
	}
	fleet := adminkernel.NodeFleetHealth{Observed: true}
	var volumes adminkernel.NodeVolumeHealth
	for _, record := range records {
		if record == nil || !record.Active() {
			continue
		}
		fleet.ActiveNodes++
		heartbeatFresh := nodekernel.HeartbeatFresh(record.UpdatedAt, now, s.heartbeatWindow)
		summaryFresh := nodekernel.SummaryFresh(record.Summary, now, s.summaryWindow)
		if !heartbeatFresh {
			fleet.StaleHeartbeatNodes++
		}
		if !summaryFresh {
			fleet.StaleSummaryNodes++
		}
		axnodedReady := record.Summary.GetComponents().GetAxnoded().GetReady() && record.Summary.GetComponents().GetAxnoded().GetState() == nodev1.ComponentState_COMPONENT_STATE_READY
		if heartbeatFresh && summaryFresh && axnodedReady {
			fleet.ReadyNodes++
		} else if heartbeatFresh && summaryFresh {
			fleet.NotReadyNodes++
		}
		if !heartbeatFresh || !summaryFresh || record.Summary == nil {
			continue
		}
		volumed := record.Summary.GetComponents().GetVolumed()
		if volumed == nil {
			continue
		}
		volumes.PublishedVolumes += int64(volumed.GetPublishedVolumeCount())
		volumes.LastReconcileStaleAllocations += int64(volumed.GetLastReconcileStaleAllocationCount())
		volumes.LastReconcileInvalidVolumes += int64(volumed.GetLastReconcileInvalidVolumeCount())
		if volumed.GetState() == nodev1.ComponentState_COMPONENT_STATE_ERROR {
			volumes.UnhealthyNodes++
			if volumes.Error == "" && strings.TrimSpace(volumed.GetLastReconcileError()) != "" {
				volumes.Error = volumed.GetLastReconcileError()
			}
		}
	}
	return fleet, volumes, nil
}
