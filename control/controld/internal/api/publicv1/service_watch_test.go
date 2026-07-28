package publicv1

import (
	"context"
	"io"
	"testing"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/metadata"
)

func TestWatchServiceStreamsSnapshotsUntilClientCancellation(t *testing.T) {
	watchStream := &staticServiceWatchStream{snapshots: []*servicev1.Service{{
		ID:      "svc-a",
		Version: 4,
		Status:  servicev1.ServiceStatus_SERVICE_STATUS_READY,
	}}}
	watcher := &staticServiceWatcher{stream: watchStream}
	server := New(Dependencies{ServiceWatcher: watcher})
	ctx, cancel := context.WithCancel(context.Background())
	stream := &captureServiceWatchServer{ctx: ctx, cancel: cancel}

	if err := server.WatchService(&servicev1.WatchServiceRequest{
		ServiceID:    "svc-a",
		AfterVersion: 3,
	}, stream); err != nil {
		t.Fatalf("WatchService() error = %v", err)
	}
	if watcher.serviceID != "svc-a" || watcher.afterVersion != 3 {
		t.Fatalf("Watch() request = (%q, %d), want (svc-a, 3)", watcher.serviceID, watcher.afterVersion)
	}
	if len(stream.responses) != 1 || stream.responses[0].GetService().GetVersion() != 4 {
		t.Fatalf("responses = %+v, want service version 4", stream.responses)
	}
	if !watchStream.closed {
		t.Fatal("watch stream was not closed")
	}
}

type staticServiceWatcher struct {
	serviceID    string
	afterVersion int64
	stream       servicekernel.WatchStream
}

func (w *staticServiceWatcher) Watch(_ context.Context, serviceID string, afterVersion int64) (servicekernel.WatchStream, error) {
	w.serviceID = serviceID
	w.afterVersion = afterVersion
	return w.stream, nil
}

type staticServiceWatchStream struct {
	snapshots []*servicev1.Service
	next      int
	closed    bool
}

func (s *staticServiceWatchStream) Next(ctx context.Context) (*servicev1.Service, error) {
	if s.next < len(s.snapshots) {
		snapshot := s.snapshots[s.next]
		s.next++
		return snapshot, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *staticServiceWatchStream) Close() {
	s.closed = true
}

type captureServiceWatchServer struct {
	ctx       context.Context
	cancel    context.CancelFunc
	responses []*servicev1.WatchServiceResponse
}

func (s *captureServiceWatchServer) Send(response *servicev1.WatchServiceResponse) error {
	s.responses = append(s.responses, response)
	s.cancel()
	return nil
}

func (s *captureServiceWatchServer) SetHeader(metadata.MD) error  { return nil }
func (s *captureServiceWatchServer) SendHeader(metadata.MD) error { return nil }
func (s *captureServiceWatchServer) SetTrailer(metadata.MD)       {}
func (s *captureServiceWatchServer) Context() context.Context     { return s.ctx }
func (s *captureServiceWatchServer) SendMsg(any) error            { return nil }
func (s *captureServiceWatchServer) RecvMsg(any) error            { return io.EOF }
