package publicv1

import (
	"context"
	"testing"

	controldcatalog "github.com/cofy-x/axern/control/controld/internal/catalog"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAgentBundleCatalogRPCs(t *testing.T) {
	server := New(Dependencies{Catalog: controldcatalog.NewStore(nil)})
	list, err := server.ListAgentBundles(context.Background(), &catalogv1.ListAgentBundlesRequest{})
	if err != nil || len(list.GetAgentBundles()) != 2 {
		t.Fatalf("ListAgentBundles() = %#v, %v", list, err)
	}
	got, err := server.GetAgentBundle(context.Background(), &catalogv1.GetAgentBundleRequest{ID: "codex", Version: "0.144.6"})
	if err != nil || got.GetAgentBundle().GetBinaryPath() != "/bin/codex" {
		t.Fatalf("GetAgentBundle() = %#v, %v", got, err)
	}
	if _, err := server.GetAgentBundle(context.Background(), &catalogv1.GetAgentBundleRequest{ID: "codex", Version: "missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetAgentBundle(missing version) code = %v", status.Code(err))
	}
	if _, err := server.GetAgentBundle(context.Background(), &catalogv1.GetAgentBundleRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetAgentBundle(empty id) code = %v", status.Code(err))
	}
}
