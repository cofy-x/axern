package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
)

const DefaultDeleteWaitTimeout = 5 * time.Minute

type ActionClient interface {
	DeleteService(context.Context, *servicev1.DeleteServiceRequest, ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error)
}

type DeleteResult struct {
	ServiceID string
	Service   *servicev1.Service
}

type DeleteParams struct {
	ServiceID   string
	Wait        bool
	WaitTimeout time.Duration
}

func (c Control) ListServiceIDs(ctx context.Context, req *servicev1.ListServicesRequest) ([]string, error) {
	resp, err := c.client.ListServices(ctx, req)
	if err != nil {
		return nil, err
	}
	return ServiceIDs(resp.GetServices()), nil
}

func (c Control) DeleteServiceIDs(ctx context.Context, serviceIDs []string) ([]string, error) {
	affectedIDs := make([]string, 0, len(serviceIDs))
	for _, id := range serviceIDs {
		result, err := c.Delete(ctx, DeleteParams{ServiceID: id})
		if err != nil {
			return nil, err
		}
		affectedIDs = append(affectedIDs, result.ServiceID)
	}
	return affectedIDs, nil
}

func (c Control) Delete(ctx context.Context, params DeleteParams) (DeleteResult, error) {
	params.ServiceID = strings.TrimSpace(params.ServiceID)
	if params.ServiceID == "" {
		return DeleteResult{}, fmt.Errorf("service id is required")
	}
	if params.WaitTimeout < 0 {
		return DeleteResult{}, fmt.Errorf("service deletion wait timeout must not be negative")
	}
	if params.Wait && c.watcher == nil {
		return DeleteResult{}, fmt.Errorf("service deletion watcher is required")
	}
	result, err := requestServiceDeletion(ctx, c.client, params.ServiceID)
	if err != nil || !params.Wait || serviceDeletionComplete(result.Service) {
		return result, err
	}
	service, waitErr := c.waitForDeletion(ctx, result.Service, params.WaitTimeout)
	if service != nil {
		result.Service = service
		result.ServiceID = service.GetID()
	}
	return result, waitErr
}

func requestServiceDeletion(ctx context.Context, client ActionClient, serviceID string) (DeleteResult, error) {
	resp, err := client.DeleteService(ctx, &servicev1.DeleteServiceRequest{ServiceID: serviceID})
	if err != nil {
		return DeleteResult{}, err
	}
	service := resp.GetService()
	if err := validateDeletionSnapshot(serviceID, 0, service); err != nil {
		return DeleteResult{}, fmt.Errorf("delete service response: %w", err)
	}
	return DeleteResult{ServiceID: service.GetID(), Service: service}, nil
}

func (c Control) waitForDeletion(ctx context.Context, initial *servicev1.Service, timeout time.Duration) (*servicev1.Service, error) {
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	last := initial
	watch, err := c.watcher.WatchService(waitCtx, initial.GetID(), initial.GetVersion())
	if err != nil {
		return last, deletionWaitError(ctx, waitCtx, last, timeout, err)
	}
	defer watch.Close()
	for {
		next, recvErr := watch.Recv()
		if recvErr != nil {
			return last, deletionWaitError(ctx, waitCtx, last, timeout, recvErr)
		}
		if err := validateDeletionSnapshot(initial.GetID(), last.GetVersion(), next); err != nil {
			return last, fmt.Errorf("watch service deletion: %w", err)
		}
		last = next
		if serviceDeletionComplete(last) {
			return last, nil
		}
	}
}

func validateDeletionSnapshot(serviceID string, afterVersion int64, service *servicev1.Service) error {
	if service == nil {
		return fmt.Errorf("response did not include a service")
	}
	if strings.TrimSpace(service.GetID()) == "" {
		return fmt.Errorf("service id is empty")
	}
	if service.GetID() != serviceID {
		return fmt.Errorf("service id %q does not match requested id %q", service.GetID(), serviceID)
	}
	if service.GetVersion() <= afterVersion {
		return fmt.Errorf("service version %d is not newer than %d", service.GetVersion(), afterVersion)
	}
	deletion := service.GetDeletionStatus()
	if deletion == nil {
		return fmt.Errorf("service %q does not include deletion status", serviceID)
	}
	switch deletion.GetPhase() {
	case servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS,
		servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES:
		if service.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING {
			return fmt.Errorf("service %q deletion phase %s has status %s", serviceID, deletion.GetPhase(), service.GetStatus())
		}
	case servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE:
		if service.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
			return fmt.Errorf("service %q completed deletion has status %s", serviceID, service.GetStatus())
		}
	default:
		return fmt.Errorf("service %q has invalid deletion phase %s", serviceID, deletion.GetPhase())
	}
	return nil
}

func serviceDeletionComplete(service *servicev1.Service) bool {
	return service.GetDeletionStatus().GetPhase() == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE
}

func deletionWaitError(parentCtx, waitCtx context.Context, last *servicev1.Service, timeout time.Duration, err error) error {
	if parentErr := parentCtx.Err(); parentErr != nil {
		if errors.Is(parentErr, context.DeadlineExceeded) {
			return fmt.Errorf(
				"command deadline exceeded waiting for service %q deletion to complete (last phase %s); deletion continues in the background",
				last.GetID(),
				last.GetDeletionStatus().GetPhase(),
			)
		}
		return parentErr
	}
	if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf(
			"timed out after %s waiting for service %q deletion to complete (last phase %s); deletion continues in the background",
			timeout,
			last.GetID(),
			last.GetDeletionStatus().GetPhase(),
		)
	}
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("watch service %q deletion ended before completion", last.GetID())
	}
	return fmt.Errorf("watch service %q deletion: %w", last.GetID(), err)
}

func ServiceIDs(services []*servicev1.Service) []string {
	ids := make([]string, 0, len(services))
	for _, svc := range services {
		if svc == nil || svc.GetID() == "" {
			continue
		}
		ids = append(ids, svc.GetID())
	}
	return ids
}
