package imageregistry

import (
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"testing"
)

func TestRegistryMirrorTransportRewritesOrigin(t *testing.T) {
	client, err := NewClient("", WithRegistryMirror("http://172.16.0.10:4001"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	transport := client.getOrCreateTransport(registryRouteMirror, "")
	transport.roundTripper.(*registryMirrorTransport).base = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.String(); got != "http://172.16.0.10:4001/v2/team/app/manifests/latest?ns=test" {
			t.Fatalf("mirrored URL = %q", got)
		}
		if got := req.Header.Get(dragonflyRegistryHeader); got != "https://registry.example.com" {
			t.Fatalf("%s = %q", dragonflyRegistryHeader, got)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
	})

	req, err := http.NewRequest(http.MethodGet, "https://registry.example.com/v2/team/app/manifests/latest?ns=test", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if got := req.URL.String(); got != "https://registry.example.com/v2/team/app/manifests/latest?ns=test" {
		t.Fatalf("original request URL was mutated: %q", got)
	}
	if got := req.Header.Get(dragonflyRegistryHeader); got != "" {
		t.Fatalf("original request header was mutated: %q", got)
	}
}

func TestRegistryMirrorTransportBypassesTokenEndpoints(t *testing.T) {
	mirror, err := url.Parse("http://172.16.0.10:4001")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	transport := &registryMirrorTransport{
		mirror: mirror,
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.URL.String(); got != "https://auth.example.com/token?scope=repository%3Ateam%2Fapp%3Apull" {
				t.Fatalf("token URL = %q", got)
			}
			if got := req.Header.Get(dragonflyRegistryHeader); got != "" {
				t.Fatalf("token request received mirror header %q", got)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
		}),
	}
	req, err := http.NewRequest(http.MethodGet, "https://auth.example.com/token?scope=repository%3Ateam%2Fapp%3Apull", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
}

func TestGetOrCreateTransportSetsConnectionLimits(t *testing.T) {
	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	transport := client.getOrCreateTransport(registryRouteMirror, "")
	if transport.base.MaxConnsPerHost != maxRegistryConnsPerHost {
		t.Fatalf("MaxConnsPerHost = %d, want %d", transport.base.MaxConnsPerHost, maxRegistryConnsPerHost)
	}
	if transport.base.MaxIdleConnsPerHost != maxRegistryIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", transport.base.MaxIdleConnsPerHost, maxRegistryIdleConnsPerHost)
	}
	if transport.base.MaxIdleConns != maxRegistryIdleConns {
		t.Fatalf("MaxIdleConns = %d, want %d", transport.base.MaxIdleConns, maxRegistryIdleConns)
	}
	if transport.base.TLSHandshakeTimeout != registryTLSHandshakeTimeout {
		t.Fatalf("TLSHandshakeTimeout = %s, want %s", transport.base.TLSHandshakeTimeout, registryTLSHandshakeTimeout)
	}
	if transport.base.ResponseHeaderTimeout != registryResponseTimeout {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.base.ResponseHeaderTimeout, registryResponseTimeout)
	}
}

func TestGetOrCreateTransportCanBypassEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "http://proxy.example:8080")
	t.Setenv("NO_PROXY", "")

	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	transport := client.getOrCreateTransport(registryRouteDirect, "")
	if transport.base.Proxy != nil {
		t.Fatal("direct transport should not use environment proxy")
	}
}

func TestRegistryTransportClosesConnectionAfterServerError(t *testing.T) {
	conn := &testConn{}
	transport := &registryTransport{
		base: newRegistryBaseTransport("", false),
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			trace := httptrace.ContextClientTrace(req.Context())
			if trace == nil || trace.GotConn == nil {
				t.Fatalf("missing got-conn trace on request")
			}
			trace.GotConn(httptrace.GotConnInfo{Conn: conn})

			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Body:       io.NopCloser(strings.NewReader("boom")),
				Request:    req,
			}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodPost, "https://registry.example/v2/test/blobs/sha256:deadbeef", nil)
	if err != nil {
		t.Fatalf("NewRequest() error: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error: %v", err)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if got := conn.closeCount.Load(); got != 1 {
		t.Fatalf("close count = %d, want 1", got)
	}
}

func TestRegistryTransportKeepsConnectionForHTTP2ServerError(t *testing.T) {
	conn := &testConn{}
	transport := &registryTransport{
		base: newRegistryBaseTransport("", false),
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			trace := httptrace.ContextClientTrace(req.Context())
			if trace == nil || trace.GotConn == nil {
				t.Fatalf("missing got-conn trace on request")
			}
			trace.GotConn(httptrace.GotConnInfo{Conn: conn})

			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Proto:      "HTTP/2.0",
				ProtoMajor: 2,
				ProtoMinor: 0,
				Body:       io.NopCloser(strings.NewReader("boom")),
				Request:    req,
			}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodPost, "https://registry.example/v2/test/blobs/sha256:deadbeef", nil)
	if err != nil {
		t.Fatalf("NewRequest() error: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error: %v", err)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if got := conn.closeCount.Load(); got != 0 {
		t.Fatalf("close count = %d, want 0", got)
	}
}
