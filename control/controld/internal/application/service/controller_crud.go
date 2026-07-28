package appservice

import (
	"context"
	"strings"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func (c *controller) Create(ctx context.Context, params servicekernel.CreateParams, now time.Time) (*servicev1.Service, error) {
	service, err := c.store.Create(ctx, params, now)
	if err == nil && service != nil {
		c.wakeReconciler(service.GetID())
	}
	return service, err
}

func (c *controller) Get(ctx context.Context, id string) (*servicev1.Service, bool, error) {
	return c.store.Get(ctx, id)
}

func (c *controller) List(ctx context.Context, filter *servicev1.ServiceListFilter) ([]*servicev1.Service, error) {
	return c.store.List(ctx, filter)
}

func (c *controller) GetReplica(ctx context.Context, serviceID, replicaID string) (*servicev1.ServiceReplica, bool, error) {
	return c.store.GetReplica(ctx, serviceID, replicaID)
}

func (c *controller) ListReplicas(ctx context.Context, serviceID string, filter *servicev1.ServiceReplicaListFilter) ([]*servicev1.ServiceReplica, error) {
	return c.store.ListReplicas(ctx, serviceID, filter)
}

func (c *controller) ListEvents(ctx context.Context, serviceID string, limit int32) ([]*servicev1.ServiceEvent, error) {
	return c.store.ListEvents(ctx, serviceID, limit)
}

func (c *controller) Update(ctx context.Context, req *servicev1.UpdateServiceRequest, now time.Time) (*servicev1.Service, error) {
	service, err := c.store.Update(ctx, req, now)
	service, err = c.syncAfterWrite(ctx, service, err, now)
	if err != nil || service == nil {
		return service, err
	}
	if rolloutTriggeredByRequest(req) && service.GetRolloutStatus() != nil && service.GetRolloutStatus().GetInProgress() {
		if emitErr := c.recordEvent(ctx, servicekernel.NewServiceEvent(
			service.GetID(),
			"",
			servicev1.ServiceEventType_SERVICE_EVENT_TYPE_ROLLOUT_STARTED,
			service.GetRolloutStatus().GetPhase(),
			service.GetRolloutStatus().GetDiagnosticCode(),
			servicekernel.FirstNonEmpty(service.GetRolloutStatus().GetDiagnosticMessage(), service.GetMessage(), "rolling update started"),
			now,
		)); emitErr != nil {
			return service, emitErr
		}
	}
	return service, nil
}

func (c *controller) Delete(ctx context.Context, params servicekernel.DeleteParams, now time.Time) (*servicev1.Service, bool, error) {
	service, ok, err := c.store.Delete(ctx, params, now)
	if err != nil || !ok || service == nil {
		return service, ok, err
	}
	if err := c.recordEvent(ctx, servicekernel.NewServiceEvent(
		service.GetID(), "", servicev1.ServiceEventType_SERVICE_EVENT_TYPE_DELETION_REQUESTED,
		servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED,
		service.GetDiagnosticCode(), "service deletion requested", now,
	)); err != nil {
		return service, true, err
	}
	c.wakeReconciler(service.GetID())
	return service, true, nil
}

func (c *controller) Purge(ctx context.Context, id string, now time.Time) (string, bool, error) {
	return c.store.Purge(ctx, id, now)
}

func rolloutTriggeredByRequest(req *servicev1.UpdateServiceRequest) bool {
	if req == nil {
		return false
	}
	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		return req.GetConfig() != nil || req.EnvironmentID != nil || req.GetReadinessProbe() != nil || req.GetLivenessProbe() != nil
	}
	for _, path := range paths {
		switch strings.TrimSpace(path) {
		case "config", "environment_id", "readiness_probe", "liveness_probe":
			return true
		}
	}
	return false
}

func (c *controller) wakeReconciler(serviceIDs ...string) {
	if c.notifyReconcile != nil {
		c.notifyReconcile(serviceIDs...)
	}
}
