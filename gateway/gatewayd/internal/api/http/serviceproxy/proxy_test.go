package serviceproxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeForwarder struct {
	handle func(net.Conn)
	err    error
}

func (f fakeForwarder) ProxyHTTP(ctx context.Context, spec nodekernel.HTTPProxySpec) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	client, server := net.Pipe()
	go f.handle(server)
	req := &http.Request{
		Method: spec.Method,
		URL: &url.URL{
			Scheme:   "http",
			Host:     "allocation.local",
			Path:     spec.Path,
			RawQuery: spec.Query,
		},
		Header: spec.Header.Clone(),
	}
	if spec.Body != nil {
		req.Body = io.NopCloser(spec.Body)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- req.Write(client)
	}()
	respCh := make(chan *http.Response, 1)
	readErrCh := make(chan error, 1)
	go func() {
		resp, err := http.ReadResponse(bufio.NewReader(client), req)
		if err != nil {
			readErrCh <- err
			return
		}
		respCh <- resp
	}()
	var resp *http.Response
	select {
	case resp = <-respCh:
	case err := <-readErrCh:
		_ = client.Close()
		<-errCh
		return nil, err
	case <-ctx.Done():
		_ = client.Close()
		<-errCh
		return nil, ctx.Err()
	}
	writeErr := <-errCh
	if writeErr != nil {
		_ = client.Close()
		return nil, writeErr
	}
	return resp, nil
}

type fakeHTTPProxy func(context.Context, nodekernel.HTTPProxySpec) (*http.Response, error)

func (f fakeHTTPProxy) ProxyHTTP(ctx context.Context, spec nodekernel.HTTPProxySpec) (*http.Response, error) {
	return f(ctx, spec)
}

type closeTrackingBody struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

func (b *closeTrackingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestProxyForwardsStatusHeadersAndBody(t *testing.T) {
	t.Parallel()
	p := New(fakeForwarder{handle: func(conn net.Conn) {
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			t.Errorf("ReadRequest() error = %v", err)
			return
		}
		if req.Header.Get("X-Hop") != "" || req.Header.Get("Upgrade") != "" || req.Header.Get("Connection") != "" {
			t.Errorf("hop-by-hop headers leaked upstream: connection=%q upgrade=%q x-hop=%q", req.Header.Get("Connection"), req.Header.Get("Upgrade"), req.Header.Get("X-Hop"))
			return
		}
		_, _ = io.Copy(io.Discard, req.Body)
		_, _ = conn.Write([]byte("HTTP/1.1 201 Created\r\nX-Test: yes\r\nX-Axern-Gateway-Error-Class: upstream\r\nContent-Length: 2\r\n\r\nok"))
	}}, Options{UpstreamTimeout: time.Second, MaxRequestBodyBytes: 1024}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "http://gateway.local/svc/default/svc/8080/hello", strings.NewReader("body"))
	req.Header.Set("Connection", "X-Hop, Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("X-Hop", "drop")
	rec := httptest.NewRecorder()
	result := serveProxyHTTP(p, rec, req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})

	if result.Status != http.StatusCreated {
		t.Fatalf("result status = %d, want 201", result.Status)
	}
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Test") != "yes" || rec.Body.String() != "ok" {
		t.Fatalf("response = code:%d header:%q body:%q", rec.Code, rec.Header().Get("X-Test"), rec.Body.String())
	}
	if rec.Header().Get(GatewayErrorClassHeader) != "" {
		t.Fatalf("gateway error header leaked from upstream: %q", rec.Header().Get(GatewayErrorClassHeader))
	}
}

func TestProxyRoundTripKeepsResponseBodyReadable(t *testing.T) {
	t.Parallel()
	p := New(fakeForwarder{handle: func(conn net.Conn) {
		defer conn.Close()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			t.Errorf("ReadRequest() error = %v", err)
			return
		}
		_, _ = io.Copy(io.Discard, req.Body)
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"))
	}}, Options{UpstreamTimeout: time.Second, MaxRequestBodyBytes: 1024}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "http://gateway.local/svc/default/svc/8080/hello", nil)
	resp, result, err := p.RoundTrip(req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if result.Status != http.StatusOK || string(body) != "ok" {
		t.Fatalf("result status = %d body = %q, want 200 ok", result.Status, string(body))
	}
}

func TestProxyResponseCloseClosesRequestBody(t *testing.T) {
	t.Parallel()
	requestBody := &closeTrackingBody{Reader: strings.NewReader("body"), closed: make(chan struct{})}
	p := New(fakeHTTPProxy(func(context.Context, nodekernel.HTTPProxySpec) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	}), Options{UpstreamTimeout: time.Second, MaxRequestBodyBytes: 1024}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "http://gateway.local/svc/default/svc/8080/hello", nil)
	req.Body = requestBody
	req.ContentLength = 4
	resp, _, err := p.RoundTrip(req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("response Body.Close() error = %v", err)
	}
	select {
	case <-requestBody.closed:
	case <-time.After(time.Second):
		t.Fatal("request body was not closed")
	}
}

func TestProxyErrorClosesRequestBody(t *testing.T) {
	t.Parallel()
	requestBody := &closeTrackingBody{Reader: strings.NewReader("body"), closed: make(chan struct{})}
	p := New(fakeHTTPProxy(func(context.Context, nodekernel.HTTPProxySpec) (*http.Response, error) {
		return nil, errors.New("upstream failed before response")
	}), Options{UpstreamTimeout: time.Second, MaxRequestBodyBytes: 1024}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "http://gateway.local/svc/default/svc/8080/hello", nil)
	req.Body = requestBody
	req.ContentLength = 4
	if _, _, err := p.RoundTrip(req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080}); err == nil {
		t.Fatal("RoundTrip() error = nil, want upstream error")
	}
	select {
	case <-requestBody.closed:
	case <-time.After(time.Second):
		t.Fatal("request body was not closed")
	}
}

func TestProxyLeaseRejectionDoesNotRetrySameAuthority(t *testing.T) {
	t.Parallel()
	var calls int
	p := New(fakeHTTPProxy(func(context.Context, nodekernel.HTTPProxySpec) (*http.Response, error) {
		calls++
		return nil, status.Error(codes.Unauthenticated, "execution lease is invalid")
	}), Options{UpstreamTimeout: time.Second, MaxRequestBodyBytes: 1024}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "http://gateway.local/svc/default/svc/8080/hello", nil)
	_, result, err := p.RoundTrip(req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("RoundTrip() error = %v, want Unauthenticated", err)
	}
	if calls != 1 {
		t.Fatalf("proxy calls = %d, want 1", calls)
	}
	if !result.LeaseRejected || !result.Invalidate {
		t.Fatalf("result = %+v, want rejected lease with invalidation", result)
	}
}

func TestProxyLeaseRejectionPreservesUnconsumedRequestBody(t *testing.T) {
	t.Parallel()
	body := &closeTrackingBody{Reader: strings.NewReader("payload"), closed: make(chan struct{})}
	p := New(fakeHTTPProxy(func(context.Context, nodekernel.HTTPProxySpec) (*http.Response, error) {
		return nil, status.Error(codes.Unauthenticated, "execution lease is invalid")
	}), Options{UpstreamTimeout: time.Second, MaxRequestBodyBytes: 1024}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://gateway.local/svc/default/svc/8080/hello", nil)
	req.Body = body
	req.ContentLength = int64(len("payload"))
	req.GetBody = nil

	_, result, err := p.RoundTrip(req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})
	if status.Code(err) != codes.Unauthenticated || !result.LeaseRejected {
		t.Fatalf("RoundTrip() result = %+v error = %v", result, err)
	}
	select {
	case <-body.closed:
		t.Fatal("request body closed before fresh lease retry")
	default:
	}
	remaining, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(remaining) != "payload" {
		t.Fatalf("remaining body = %q, want payload", remaining)
	}
	_ = body.Close()
}

func TestProxyRejectsOversizedRequestBody(t *testing.T) {
	t.Parallel()
	p := New(fakeForwarder{}, Options{MaxRequestBodyBytes: 4}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://gateway.local/svc/default/svc/8080/hello", strings.NewReader("too-large"))
	req.ContentLength = 9
	rec := httptest.NewRecorder()

	result := serveProxyHTTP(p, rec, req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})

	if rec.Code != http.StatusRequestEntityTooLarge || result.ErrorClass != "body_too_large" {
		t.Fatalf("result = code:%d class:%q", rec.Code, result.ErrorClass)
	}
	if rec.Header().Get(GatewayErrorClassHeader) != "body_too_large" {
		t.Fatalf("gateway error header = %q", rec.Header().Get(GatewayErrorClassHeader))
	}
}

func TestProxyUpstreamTimeoutReturnsGatewayTimeout(t *testing.T) {
	t.Parallel()
	p := New(fakeForwarder{handle: func(conn net.Conn) {
		defer conn.Close()
		time.Sleep(time.Second)
	}}, Options{UpstreamTimeout: 10 * time.Millisecond, MaxRequestBodyBytes: 1024}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://gateway.local/svc/default/svc/8080/hello", nil)
	rec := httptest.NewRecorder()

	result := serveProxyHTTP(p, rec, req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})

	if rec.Code != http.StatusGatewayTimeout || result.ErrorClass != "timeout" {
		t.Fatalf("result = code:%d class:%q", rec.Code, result.ErrorClass)
	}
	if rec.Header().Get(GatewayErrorClassHeader) != "timeout" {
		t.Fatalf("gateway error header = %q", rec.Header().Get(GatewayErrorClassHeader))
	}
	if result.EndpointRetryable {
		t.Fatal("EndpointRetryable = true, want false for generic upstream timeout")
	}
}

func TestProxyConnectFailureIsEndpointRetryable(t *testing.T) {
	t.Parallel()
	p := New(fakeForwarder{
		err: errors.New("rpc error: code = DeadlineExceeded desc = connect allocation port: dial tcp 172.17.0.2:8080: i/o timeout"),
	}, Options{UpstreamTimeout: time.Second, MaxRequestBodyBytes: 1024}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://gateway.local/svc/default/svc/8080/hello", nil)
	rec := httptest.NewRecorder()

	result := serveProxyHTTP(p, rec, req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})

	if rec.Code != http.StatusGatewayTimeout || result.ErrorClass != "timeout" || !result.EndpointRetryable {
		t.Fatalf("result = code:%d class:%q endpoint_retryable:%v", rec.Code, result.ErrorClass, result.EndpointRetryable)
	}
}

func TestProxyLeaseErrorInvalidatesRoute(t *testing.T) {
	t.Parallel()
	p := New(fakeForwarder{
		err: status.Error(codes.Unauthenticated, "execution lease is invalid"),
	}, Options{UpstreamTimeout: time.Second, MaxRequestBodyBytes: 1024}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://gateway.local/svc/default/svc/8080/hello", strings.NewReader("body"))
	rec := httptest.NewRecorder()

	result := serveProxyHTTP(p, rec, req, &gatewayv1.ServiceRouteEndpoint{AllocationID: "alloc-a", Attempt: 1, ContainerPort: 8080})

	if rec.Code != http.StatusBadGateway || result.ErrorClass != "lease" || !result.Invalidate || !result.LeaseRejected {
		t.Fatalf("result = code:%d class:%q invalidate:%v", rec.Code, result.ErrorClass, result.Invalidate)
	}
	if rec.Header().Get(GatewayErrorClassHeader) != "lease" {
		t.Fatalf("gateway error header = %q", rec.Header().Get(GatewayErrorClassHeader))
	}
}

func serveProxyHTTP(p *Proxy, w http.ResponseWriter, r *http.Request, ep *gatewayv1.ServiceRouteEndpoint) Result {
	resp, result, err := p.RoundTrip(r, ep)
	if err != nil {
		p.WriteError(w, result)
		return result
	}
	p.WriteResponse(w, resp)
	return result
}
