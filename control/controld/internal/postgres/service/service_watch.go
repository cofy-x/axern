package pgservice

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *PGStore) Watch(ctx context.Context, serviceID string, afterVersion int64) (servicekernel.WatchStream, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "service_id is required")
	}
	if afterVersion < 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "after_version must be non-negative")
	}

	subscription, err := s.watches.subscribe(ctx, serviceID)
	if err != nil {
		return nil, serviceWatchError(err)
	}
	current, ok, err := s.Get(ctx, serviceID)
	if err != nil {
		subscription.close()
		return nil, err
	}
	if !ok {
		subscription.close()
		return nil, grpcstatus.Error(codes.NotFound, "service not found")
	}
	if afterVersion > current.GetVersion() {
		subscription.close()
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "after_version %d exceeds current service version %d", afterVersion, current.GetVersion())
	}

	watch := &serviceWatch{
		store:        s,
		serviceID:    serviceID,
		lastVersion:  afterVersion,
		subscription: subscription,
	}
	if current.GetVersion() > afterVersion {
		watch.pending = current
	}
	return watch, nil
}

func (s *PGStore) Close() {
	if s != nil && s.watches != nil {
		s.watches.close()
	}
}

func (s *PGStore) WatchStats() WatchStats {
	if s == nil || s.watches == nil {
		return WatchStats{}
	}
	return s.watches.stats()
}

type serviceWatch struct {
	store        *PGStore
	serviceID    string
	lastVersion  int64
	pending      *servicev1.Service
	subscription *watchSubscription
	closeOnce    sync.Once
}

func (w *serviceWatch) Next(ctx context.Context) (*servicev1.Service, error) {
	if w.pending != nil {
		current := w.pending
		w.pending = nil
		w.lastVersion = current.GetVersion()
		return current, nil
	}
	for {
		if err := w.subscription.wait(ctx); err != nil {
			return nil, serviceWatchError(err)
		}
		current, ok, err := w.store.Get(ctx, w.serviceID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, grpcstatus.Error(codes.NotFound, "service not found")
		}
		if current.GetVersion() <= w.lastVersion {
			continue
		}
		w.lastVersion = current.GetVersion()
		return current, nil
	}
}

func (w *serviceWatch) Close() {
	if w == nil {
		return
	}
	w.closeOnce.Do(func() {
		w.subscription.close()
	})
}

func serviceWatchError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, io.EOF):
		return err
	case errors.Is(err, errWatchListenerUnavailable):
		return grpcstatus.Error(codes.Unavailable, "service watch is temporarily unavailable")
	default:
		return err
	}
}
