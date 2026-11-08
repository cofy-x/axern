package pgworkspace

import (
	"context"
	"strings"
	"testing"
	"time"

	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOversizedWorkspaceNamespaceIsRejectedBeforeStorageAdmission(t *testing.T) {
	store := NewStore(nil, time.Second, time.Second)
	_, err := store.CreateWorkspace(context.Background(), &workspacev1.CreateWorkspaceRequest{Namespace: strings.Repeat("界", 1366), NodeID: "node-one", CapacityBytes: 512, InodeLimit: 1}, time.Now())
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized namespace reached database admission: %v", err)
	}
}
