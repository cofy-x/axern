package dashboard

import (
	"context"

	appadmin "github.com/cofy-x/axern/apps/cli/internal/application/admin"
	appquota "github.com/cofy-x/axern/apps/cli/internal/application/quota"
	appservice "github.com/cofy-x/axern/apps/cli/internal/application/service"
	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
)

type Control struct {
	services  appservice.Control
	tunnels   apptunnel.Control
	quotas    appquota.Control
	retries   appadmin.AllocationLifecycleControl
	audit     appadmin.AuditControl
	health    appadmin.ReliabilityControl
	hasHealth bool
}

type Clients struct {
	Service             appservice.ServiceClient
	Tunnel              apptunnel.TunnelClient
	Quota               appquota.Client
	AllocationLifecycle appadmin.AllocationLifecycleClient
	Audit               appadmin.AuditClient
	Reliability         appadmin.ReliabilityClient
}

func New(clients Clients) Control {
	control := Control{
		services: appservice.New(clients.Service),
		tunnels:  apptunnel.New(clients.Tunnel),
		quotas:   appquota.New(clients.Quota),
		retries:  appadmin.NewAllocationLifecycle(clients.AllocationLifecycle),
		audit:    appadmin.NewAudit(clients.Audit),
	}
	if clients.Reliability != nil {
		control.health = appadmin.NewReliability(clients.Reliability)
		control.hasHealth = true
	}
	return control
}

func (c Control) Summary(ctx context.Context) (Summary, error) {
	services, err := c.Services(ctx)
	if err != nil {
		return Summary{}, err
	}
	tunnels, err := c.Tunnels(ctx, TunnelListParams{})
	if err != nil {
		return Summary{}, err
	}
	quotas, err := c.Quotas(ctx)
	if err != nil {
		return Summary{}, err
	}
	var reliability *ReliabilitySummary
	if c.hasHealth {
		health, err := c.health.Health(ctx)
		if err != nil {
			return Summary{}, err
		}
		reliability = NewReliabilitySummary(health.GetHealth())
	}
	var out Summary
	for _, svc := range services {
		out.Services.Total++
		switch svc.Status {
		case "ready":
			out.Services.Ready++
		case "degraded":
			out.Services.Degraded++
		case "failed":
			out.Services.Failed++
		case "reconciling":
			out.Services.Reconciling++
		}
		if svc.AdmissionSummary != "" {
			out.Services.AdmissionBlocked++
		}
	}
	for _, session := range tunnels {
		out.Tunnels.Total++
		switch session.Status {
		case "running":
			out.Tunnels.Running++
		case "pending":
			out.Tunnels.Pending++
		case "degraded":
			out.Tunnels.Degraded++
		case "failed":
			out.Tunnels.Failed++
		}
	}
	for _, quota := range quotas {
		out.Quotas.Namespaces++
		if quota.CPUMilliLimit != nil {
			out.Quotas.CPUConstrained++
		}
		if quota.MemoryBytesLimit != nil {
			out.Quotas.MemoryConstrained++
		}
		if quota.EphemeralStorageBytesLimit != nil {
			out.Quotas.EphemeralStorageConstrained++
		}
		if quota.CPUUsagePercent != nil && *quota.CPUUsagePercent >= 80 {
			out.Quotas.CPUPressure++
		}
		if quota.MemoryUsagePercent != nil && *quota.MemoryUsagePercent >= 80 {
			out.Quotas.MemoryPressure++
		}
		if quota.EphemeralStorageUsagePercent != nil && *quota.EphemeralStorageUsagePercent >= 80 {
			out.Quotas.EphemeralStoragePressure++
		}
	}
	out.Reliability = reliability
	return out, nil
}

func (c Control) ReconcileHealth(ctx context.Context) (ReconcileHealth, error) {
	if !c.hasHealth {
		return ReconcileHealth{Components: []ReconcileComponentHealth{}}, nil
	}
	response, err := c.health.Health(ctx)
	if err != nil {
		return ReconcileHealth{}, err
	}
	components := response.GetHealth().GetReconcileComponents()
	out := ReconcileHealth{Components: make([]ReconcileComponentHealth, 0, len(components))}
	for _, component := range components {
		out.Components = append(out.Components, ReconcileComponentHealth{
			Component:           component.GetComponent(),
			Running:             component.GetRunning(),
			LastStartedAt:       formatTime(component.GetLastStartedAt()),
			LastSuccessAt:       formatTime(component.GetLastSuccessAt()),
			LastErrorAt:         formatTime(component.GetLastErrorAt()),
			LastError:           component.GetLastError(),
			ConsecutiveFailures: component.GetConsecutiveFailures(),
		})
	}
	return out, nil
}
