package l7inspect

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayHTTPAuthorizesEveryFramedRequest(t *testing.T) {
	for _, tc := range []struct {
		name, wire       string
		statuses         []int
		upstreamRequests int32
	}{
		{"pipeline_host_switch", "GET / HTTP/1.1\r\nHost: allowed.test\r\n\r\nGET / HTTP/1.1\r\nHost: denied.test\r\n\r\n", []int{200, 403}, 1},
		{"pipeline_allowed", "GET / HTTP/1.1\r\nHost: allowed.test\r\n\r\nGET / HTTP/1.1\r\nHost: allowed.test\r\nConnection: close\r\n\r\n", []int{200, 200}, 2},
		{"chunked_body", "POST / HTTP/1.1\r\nHost: allowed.test\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nbody\r\n0\r\n\r\nGET / HTTP/1.1\r\nHost: denied.test\r\n\r\n", []int{200, 403}, 1},
		{"fixed_body", "POST / HTTP/1.1\r\nHost: allowed.test\r\nContent-Length: 4\r\n\r\nbodyGET / HTTP/1.1\r\nHost: denied.test\r\n\r\n", []int{200, 403}, 1},
		{"upgrade", "GET / HTTP/1.1\r\nHost: allowed.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", []int{403}, 0},
		{"connect", "CONNECT allowed.test:80 HTTP/1.1\r\nHost: allowed.test\r\n\r\n", []int{403}, 0},
		{"direct_ip", "GET / HTTP/1.1\r\nHost: 192.0.2.1\r\n\r\n", []int{403}, 0},
		{"oversized_header", "GET / HTTP/1.1\r\nHost: allowed.test\r\nX-Large: " + strings.Repeat("x", 2*DefaultMaxHTTPHeaderBytes) + "\r\n\r\n", []int{431}, 0},
		{"chunked_trailer", "POST / HTTP/1.1\r\nHost: allowed.test\r\nTransfer-Encoding: chunked\r\nTrailer: X-End\r\n\r\n4\r\nbody\r\n0\r\nX-End: done\r\n\r\nGET / HTTP/1.1\r\nHost: denied.test\r\n\r\n", []int{200, 403}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Host != "allowed.test" {
					t.Errorf("unauthorized host reached backend: %s", r.Host)
				}
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = io.WriteString(w, "ok")
			}))
			defer backend.Close()
			client, relay := net.Pipe()
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			done := make(chan struct{})
			go func() {
				defer close(done)
				RelayHTTP(relay, time.Second, func(r HTTPRequest) bool { return r.Host == "allowed.test" }, func(ctx context.Context) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(backend.URL, "http://"))
				})
			}()
			go func() { _, _ = io.WriteString(client, tc.wire) }()
			reader := bufio.NewReader(client)
			for _, status := range tc.statuses {
				response, err := http.ReadResponse(reader, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode != status {
					t.Fatalf("status %d, want %d", response.StatusCode, status)
				}
			}
			_ = client.Close()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("relay leaked")
			}
			if calls.Load() != tc.upstreamRequests {
				t.Fatalf("backend calls = %d, want %d", calls.Load(), tc.upstreamRequests)
			}
		})
	}
}

func TestRelayHTTPRechecksAuthorizationOnKeepAlive(t *testing.T) {
	var checks atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer backend.Close()
	client, relay := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		RelayHTTP(relay, time.Second, func(HTTPRequest) bool { return checks.Add(1) == 1 }, func(ctx context.Context) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(backend.URL, "http://"))
		})
	}()
	reader := bufio.NewReader(client)
	for _, want := range []int{200, 403} {
		_, _ = io.WriteString(client, "GET / HTTP/1.1\r\nHost: allowed.test\r\n\r\n")
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("status = %d, want %d", response.StatusCode, want)
		}
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay leaked")
	}
}
