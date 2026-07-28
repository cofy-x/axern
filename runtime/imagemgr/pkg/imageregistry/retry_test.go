package imageregistry

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryTransportRetriesTimeoutError(t *testing.T) {
	restore := setRegistryRetryDelays(t, time.Millisecond, time.Millisecond)
	defer restore()

	var attempts atomic.Int32
	transport := &registryTransport{
		base: newRegistryBaseTransport("", false),
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempt := attempts.Add(1)
			if attempt < 3 {
				return nil, testTimeoutError{msg: "net/http: TLS handshake timeout"}
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    req,
			}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodGet, "https://registry.example/v2/test/manifests/latest", nil)
	if err != nil {
		t.Fatalf("NewRequest() error: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempt count = %d, want 3", got)
	}
}

func TestRegistryTransportRetriesServerErrorResponse(t *testing.T) {
	restore := setRegistryRetryDelays(t, time.Millisecond, time.Millisecond)
	defer restore()

	firstConn := &testConn{}
	var attempts atomic.Int32
	transport := &registryTransport{
		base: newRegistryBaseTransport("", false),
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			trace := httptrace.ContextClientTrace(req.Context())
			if trace == nil || trace.GotConn == nil {
				t.Fatalf("missing got-conn trace on request")
			}
			attempt := attempts.Add(1)
			conn := &testConn{}
			if attempt == 1 {
				conn = firstConn
			}
			trace.GotConn(httptrace.GotConnInfo{Conn: conn})

			statusCode := http.StatusOK
			body := "ok"
			if attempt == 1 {
				statusCode = http.StatusServiceUnavailable
				body = "retry"
			}

			return &http.Response{
				StatusCode: statusCode,
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodGet, "https://registry.example/v2/test/manifests/latest", nil)
	if err != nil {
		t.Fatalf("NewRequest() error: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempt count = %d, want 2", got)
	}
	if got := firstConn.closeCount.Load(); got != 1 {
		t.Fatalf("first conn close count = %d, want 1", got)
	}
}

func TestRegistryTransportDoesNotRetryNonRetryableError(t *testing.T) {
	restore := setRegistryRetryDelays(t, time.Millisecond, time.Millisecond)
	defer restore()

	var attempts atomic.Int32
	transport := &registryTransport{
		base: newRegistryBaseTransport("", false),
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts.Add(1)
			return nil, errors.New("permission denied")
		}),
	}

	req, err := http.NewRequest(http.MethodGet, "https://registry.example/v2/test/manifests/latest", nil)
	if err != nil {
		t.Fatalf("NewRequest() error: %v", err)
	}

	if _, err := transport.RoundTrip(req); err == nil {
		t.Fatal("RoundTrip() error = nil, want error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempt count = %d, want 1", got)
	}
}
