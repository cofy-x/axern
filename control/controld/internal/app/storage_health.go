package app

import (
	"context"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
)

type nodeHealthSource struct {
	store           nodeHealthRecordSource
	heartbeatWindow time.Duration
	summaryWindow   time.Duration
}

type nodeHealthRecordSource interface {
	ListNodes(ctx context.Context, filter adminkernel.NodeListFilter) ([]*nodekernel.Record, error)
}

func (s nodeHealthSource) NodeHealth(ctx context.Context, now time.Time) (adminkernel.NodeFleetHealth, error) {
	if s.store == nil {
		return adminkernel.NodeFleetHealth{}, nil
	}
	records, err := s.store.ListNodes(ctx, adminkernel.NodeListFilter{Lifecycle: nodekernel.LifecycleActive})
	if err != nil {
		return adminkernel.NodeFleetHealth{}, err
	}
	fleet := adminkernel.NodeFleetHealth{Observed: true}
	for _, record := range records {
		if record == nil || !record.Active() {
			continue
		}
		fleet.ActiveNodes++
		heartbeatFresh := nodekernel.HeartbeatFresh(record.LastHeartbeatAt, now, s.heartbeatWindow)
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

	}
	return fleet, nil
}
