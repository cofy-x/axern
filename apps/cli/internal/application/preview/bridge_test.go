package preview

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	axern "github.com/cofy-x/axern/sdk/go"
	"google.golang.org/grpc/codes"
)

type fetchFunc func(context.Context, uint32, string, string) (axern.HTTPPreview, error)

func (f fetchFunc) PreviewHTTP(ctx context.Context, port uint32, method, target string) (axern.HTTPPreview, error) {
	return f(ctx, port, method, target)
}

func TestBrowserPreviewDropsCrossOriginAndInvalidRequests(t *testing.T) {
	reached := 0
	handler := newHandler(fetchFunc(func(_ context.Context, port uint32, method, target string) (axern.HTTPPreview, error) {
		reached++
		if port != 8080 || method != "GET" || target != "/page?q=yes" {
			t.Errorf("guest request = %d %s %s", port, method, target)
		}
		return axern.HTTPPreview{Status: 200, ContentType: "text/html", Body: []byte("<p>Hello</p>")}, nil
	}), "unique.localhost:45678", 8080, func() {})
	for _, tc := range []struct {
		name, method, host, site string
		want                     int
	}{
		{"authorized navigation", "GET", "unique.localhost:45678", "none", 200},
		{"plain loopback origin", "GET", "127.0.0.1:45678", "none", 404},
		{"cross-origin fetch", "GET", "unique.localhost:45678", "cross-site", 404},
		{"unsafe method", "POST", "unique.localhost:45678", "same-origin", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/page?q=yes", nil)
			req.Host = tc.host
			req.Header.Set("Sec-Fetch-Site", tc.site)
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			if resp.Code != tc.want {
				t.Fatalf("HTTP %d, want %d", resp.Code, tc.want)
			}
			if tc.want == 200 && (resp.Header().Get("Cache-Control") != "no-store" || !strings.Contains(resp.Header().Get("Content-Security-Policy"), "worker-src 'none'") || resp.Body.String() != "<p>Hello</p>") {
				t.Fatalf("unexpected preview response: %v %s", resp.Header(), resp.Body.String())
			}
		})
	}
	if reached != 1 {
		t.Fatalf("guest received %d requests, want one", reached)
	}
}

func TestBrowserPreviewClosesListenerWhenAuthorityIsRevoked(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() {
		done <- Serve(ctx, fetchFunc(func(context.Context, uint32, string, string) (axern.HTTPPreview, error) {
			return axern.HTTPPreview{}, &axern.RPCError{Code: codes.NotFound}
		}), 8080, func(address string) error {
			ready <- address
			return nil
		})
	}()
	var address string
	select {
	case address = <-ready:
	case err := <-done:
		t.Fatalf("preview could not listen: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	origin, err := url.Parse(address)
	if err != nil || !strings.HasSuffix(origin.Hostname(), ".localhost") {
		t.Fatalf("preview address = %q, error = %v", address, err)
	}
	target := "http://" + net.JoinHostPort("127.0.0.1", origin.Port()) + "/"
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = origin.Host
	httpClient := &http.Client{Transport: &http.Transport{}}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("preview returned %d, want Gone", resp.StatusCode)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("revoked preview kept serving")
	}
	if resp, err := httpClient.Get(target); err == nil {
		_ = resp.Body.Close()
		t.Fatal("listener accepted a connection after revocation")
	}
}
