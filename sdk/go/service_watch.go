package axernsdk

import (
	"context"
	"fmt"
	"io"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	serviceWatchRetryMinDelay = 100 * time.Millisecond
	serviceWatchRetryMaxDelay = 2 * time.Second
)

// ServiceWatch is a resumable stream of monotonically newer service snapshots.
type ServiceWatch interface {
	Recv() (*servicev1.Service, error)
	Close()
}

// WatchService watches service snapshots newer than afterVersion.
func (c *Client) WatchService(ctx context.Context, serviceID string, afterVersion int64) (ServiceWatch, error) {
	if isBlank(serviceID) {
		return nil, requiredError("service_id")
	}
	if afterVersion < 0 {
		return nil, validationError("after_version", "must be non-negative")
	}
	watchCtx, cancel := context.WithCancel(ctx)
	return &serviceWatch{
		ctx:         watchCtx,
		cancel:      cancel,
		client:      c.services,
		serviceID:   serviceID,
		lastVersion: afterVersion,
		retryDelay:  serviceWatchRetryMinDelay,
	}, nil
}

type serviceWatch struct {
	ctx         context.Context
	cancel      context.CancelFunc
	client      servicev1.ServiceControlClient
	serviceID   string
	lastVersion int64
	retryDelay  time.Duration
	stream      servicev1.ServiceControl_WatchServiceClient
}

func (w *serviceWatch) Recv() (*servicev1.Service, error) {
	for {
		if err := w.ctx.Err(); err != nil {
			return nil, err
		}
		if w.stream == nil {
			stream, err := w.client.WatchService(w.ctx, &servicev1.WatchServiceRequest{
				ServiceID:    w.serviceID,
				AfterVersion: w.lastVersion,
			})
			if err != nil {
				if w.retryable(err) {
					continue
				}
				if ctxErr := w.ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				return nil, mapRPCError(err, "watch service", "")
			}
			w.stream = stream
		}

		response, err := w.stream.Recv()
		if err != nil {
			w.stream = nil
			if w.retryable(err) {
				continue
			}
			if ctxErr := w.ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, mapRPCError(err, "watch service", "")
		}
		service := response.GetService()
		if service == nil {
			return nil, fmt.Errorf("watch service failed: response did not include a service")
		}
		if service.GetVersion() <= w.lastVersion {
			continue
		}
		w.lastVersion = service.GetVersion()
		w.retryDelay = serviceWatchRetryMinDelay
		return service, nil
	}
}

func (w *serviceWatch) Close() {
	if w != nil && w.cancel != nil {
		w.cancel()
	}
}

func (w *serviceWatch) retryable(err error) bool {
	if err != io.EOF && grpcstatus.Code(err) != codes.Unavailable {
		return false
	}
	timer := time.NewTimer(w.retryDelay)
	defer timer.Stop()
	select {
	case <-w.ctx.Done():
		return false
	case <-timer.C:
	}
	w.retryDelay *= 2
	if w.retryDelay > serviceWatchRetryMaxDelay {
		w.retryDelay = serviceWatchRetryMaxDelay
	}
	return true
}
