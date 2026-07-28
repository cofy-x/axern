package service

import (
	"context"
	"fmt"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

const DefaultCreateWaitTimeout = 5 * time.Minute

type WaitSnapshot struct {
	Service  *servicev1.Service
	Replicas []*servicev1.ServiceReplica
	Events   []*servicev1.ServiceEvent
}

func (c Control) WaitReady(ctx context.Context, serviceID string, timeout time.Duration, onUpdate func(WaitSnapshot)) (WaitSnapshot, error) {
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var last WaitSnapshot
	for {
		snapshot, err := c.loadWaitSnapshot(waitCtx, serviceID)
		if err != nil {
			return last, err
		}
		last = snapshot
		if onUpdate != nil {
			onUpdate(snapshot)
		}
		if serviceReady(snapshot.Service) {
			return snapshot, nil
		}
		if serviceTerminalFailure(snapshot.Service) {
			message := snapshot.Service.GetMessage()
			if message == "" {
				return snapshot, fmt.Errorf("service %s reached %s", serviceID, snapshot.Service.GetStatus().String())
			}
			return snapshot, fmt.Errorf("service %s reached %s: %s", serviceID, snapshot.Service.GetStatus().String(), message)
		}
		select {
		case <-waitCtx.Done():
			return last, fmt.Errorf("timed out waiting for service %s to become ready", serviceID)
		case <-ticker.C:
		}
	}
}

func (c Control) loadWaitSnapshot(ctx context.Context, serviceID string) (WaitSnapshot, error) {
	serviceResp, err := c.GetService(ctx, serviceID)
	if err != nil {
		return WaitSnapshot{}, err
	}
	replicasResp, err := c.ListReplicas(ctx, serviceID, &servicev1.ServiceReplicaListFilter{
		View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT,
	})
	if err != nil {
		return WaitSnapshot{}, err
	}
	eventsResp, err := c.ListEvents(ctx, serviceID, 5)
	if err != nil {
		return WaitSnapshot{}, err
	}
	return WaitSnapshot{
		Service:  serviceResp.GetService(),
		Replicas: replicasResp.GetReplicas(),
		Events:   eventsResp.GetEvents(),
	}, nil
}

func serviceReady(service *servicev1.Service) bool {
	return service != nil && service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_READY && service.GetReadyReplicas() >= service.GetReplicas()
}

func serviceTerminalFailure(service *servicev1.Service) bool {
	return service != nil && service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_FAILED
}
