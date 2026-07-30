package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/auth"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"google.golang.org/grpc"
)

func TestHandlerRequiresDevToken(t *testing.T) {
	handler := newTestHandler(t, t.TempDir(), nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandlerServesDashboardWithTokenAndMissingVendorNotice(t *testing.T) {
	handler := newTestHandler(t, t.TempDir(), nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard?token=secret", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "gatewayd") {
		t.Fatalf("dashboard body = %q, want gatewayd marker", body)
	}
	if !strings.Contains(body, "make gateway-dashboard-assets") {
		t.Fatalf("dashboard body = %q, want missing vendor command", body)
	}
	if !strings.Contains(body, "/dashboard/assets/style.css?token=secret") {
		t.Fatalf("dashboard body = %q, want tokenized asset URL", body)
	}
}

func TestHandlerServesEmbeddedAssets(t *testing.T) {
	handler := newTestHandler(t, t.TempDir(), nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/assets/app.js?token=secret", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "WebSocket") {
		t.Fatalf("app.js body = %q, want websocket code", rec.Body.String())
	}
}

func TestHandlerServesVendorAssetsFromDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xterm.js"), []byte("xterm js"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "xterm.css"), []byte("xterm css"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "addon-fit.js"), []byte("fit js"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, dir, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/vendor/xterm.js?token=secret", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.TrimSpace(rec.Body.String()) != "xterm js" {
		t.Fatalf("vendor body = %q, want xterm js", rec.Body.String())
	}
}

func TestHandlerServesCurrentReadyServiceReplicas(t *testing.T) {
	resolver := &fakeReplicaResolver{replicas: []serviceReplicaCandidate{{AllocationID: "alloc-ready", NodeID: "node-a"}}}
	handler := newTestHandler(t, t.TempDir(), resolver)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/api/services/svc-123/replicas?token=secret", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body serviceReplicaResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ServiceID != "svc-123" {
		t.Fatalf("service id = %q, want svc-123", body.ServiceID)
	}
	if len(body.Replicas) != 1 || body.Replicas[0].AllocationID != "alloc-ready" || body.Replicas[0].NodeID != "node-a" {
		t.Fatalf("replicas = %#v, want alloc-ready only", body.Replicas)
	}
	if resolver.last != "svc-123" {
		t.Fatalf("service id = %q, want svc-123", resolver.last)
	}
}

func newTestHandler(t *testing.T, vendorDir string, resolver ServiceReplicaResolver) *Handler {
	t.Helper()
	handler, err := New(auth.DevToken{Token: "secret"}, vendorDir, resolver)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestGatewayServiceReplicaResolverRequestsTargets(t *testing.T) {
	client := &fakeGatewayControlClient{resp: &gatewayv1.ResolveServiceReplicaTargetsResponse{Replicas: []*gatewayv1.ServiceReplicaTarget{{AllocationID: "alloc-ready", NodeID: "node-a"}}}}
	resolver := NewServiceReplicaResolver(client)

	got, err := resolver.CurrentReadyReplicas(context.Background(), "svc-123")

	if err != nil {
		t.Fatalf("CurrentReadyReplicas returned error: %v", err)
	}
	if len(got) != 1 || got[0].AllocationID != "alloc-ready" {
		t.Fatalf("replicas = %#v, want alloc-ready only", got)
	}
	if client.last.GetServiceID() != "svc-123" {
		t.Fatalf("request = %#v, want targets for svc-123", client.last)
	}
}

type fakeReplicaResolver struct {
	last     string
	replicas []serviceReplicaCandidate
}

func (f *fakeReplicaResolver) CurrentReadyReplicas(_ context.Context, serviceID string) ([]serviceReplicaCandidate, error) {
	f.last = serviceID
	return f.replicas, nil
}

type fakeGatewayControlClient struct {
	last *gatewayv1.ResolveServiceReplicaTargetsRequest
	resp *gatewayv1.ResolveServiceReplicaTargetsResponse
}

func (f *fakeGatewayControlClient) ResolveServiceReplicaTargets(_ context.Context, req *gatewayv1.ResolveServiceReplicaTargetsRequest, _ ...grpc.CallOption) (*gatewayv1.ResolveServiceReplicaTargetsResponse, error) {
	f.last = req
	return f.resp, nil
}
