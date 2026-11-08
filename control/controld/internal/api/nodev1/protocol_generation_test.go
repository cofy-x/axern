package nodev1

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	controlnodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type snapshotGenerationServer struct {
	controlnodev1.UnimplementedNodeControlServer
	calls atomic.Int32
}

func (s *snapshotGenerationServer) PublishNodeSnapshot(context.Context, *controlnodev1.PublishNodeSnapshotRequest) (*controlnodev1.PublishNodeSnapshotResponse, error) {
	s.calls.Add(1)
	return &controlnodev1.PublishNodeSnapshotResponse{}, nil
}

func TestRetiredNodeReportCannotPublishAnOCISnapshot(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	handler := &snapshotGenerationServer{}
	controlnodev1.RegisterNodeControlServer(server, handler)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	t.Cleanup(func() { _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///node-generation",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request := &controlnodev1.PublishNodeSnapshotRequest{NodeID: "node-generation"}
	err = connection.Invoke(ctx, "/axern.private.control.node.v1.NodeControl/ReportNode", request, &controlnodev1.PublishNodeSnapshotResponse{})
	if status.Code(err) != codes.Unimplemented || handler.calls.Load() != 0 {
		t.Fatalf("retired report: error=%v accepted=%d", err, handler.calls.Load())
	}
	if _, err := controlnodev1.NewNodeControlClient(connection).PublishNodeSnapshot(ctx, request); err != nil || handler.calls.Load() != 1 {
		t.Fatalf("current snapshot: error=%v accepted=%d", err, handler.calls.Load())
	}
}
