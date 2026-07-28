package axernsdk

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
)

func TestServiceWatchResumesAndSuppressesDuplicateVersions(t *testing.T) {
	fake := &fakeAxernServer{
		watchScripts: [][]int64{{2}, {2, 3}},
		watchErrors:  []codes.Code{codes.Unavailable, codes.OK},
	}
	server, dialer := newBufconnServer(t, fake)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := NewClient(ctx, "bufnet", WithDialOptions(
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	watch, err := client.WatchService(ctx, "svc-1", 1)
	if err != nil {
		t.Fatalf("WatchService() error = %v", err)
	}
	defer watch.Close()
	first, err := watch.Recv()
	if err != nil {
		t.Fatalf("Recv(first) error = %v", err)
	}
	if first.GetVersion() != 2 {
		t.Fatalf("first version = %d, want 2", first.GetVersion())
	}
	second, err := watch.Recv()
	if err != nil {
		t.Fatalf("Recv(second) error = %v", err)
	}
	if second.GetVersion() != 3 {
		t.Fatalf("second version = %d, want 3", second.GetVersion())
	}
	fake.watchMu.Lock()
	calls := fake.watchCalls
	fake.watchMu.Unlock()
	if calls != 2 {
		t.Fatalf("watch calls = %d, want 2", calls)
	}
}

func TestWatchServiceRejectsInvalidResumeVersion(t *testing.T) {
	client := &Client{}
	if _, err := client.WatchService(context.Background(), "svc-1", -1); err == nil {
		t.Fatal("WatchService() accepted a negative after_version")
	}
}
