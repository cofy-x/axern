package httpapi

import "testing"

func TestParseServicePath(t *testing.T) {
	t.Parallel()
	got, ok := parseServicePath("/svc/default/svc-123/http/api/v1")
	if !ok {
		t.Fatal("parseServicePath() ok = false")
	}
	if got.Namespace != "default" || got.ServiceID != "svc-123" || got.PortRef != "http" || got.Upstream != "/api/v1" {
		t.Fatalf("parseServicePath() = %#v", got)
	}
}

func TestParseServicePathRootUpstream(t *testing.T) {
	t.Parallel()
	got, ok := parseServicePath("/svc/default/svc-123/8080")
	if !ok {
		t.Fatal("parseServicePath() ok = false")
	}
	if got.Upstream != "/" {
		t.Fatalf("upstream = %q, want /", got.Upstream)
	}
}
