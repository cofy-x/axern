package networking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type HTTPProxyStream interface {
	TargetID() string
	Port() int32
	Method() string
	Path() string
	Query() string
	Header() http.Header
	HasBody() bool
	ContentLength() int64
	RecvBody() ([]byte, error)
	SendHead(statusCode int, header http.Header) error
	SendBody([]byte) error
	SendTrailers(http.Header) error
	Context() context.Context
}

func (c *Coordinator) ProxyHTTP(stream HTTPProxyStream) (retErr error) {
	started := time.Now()
	result := "ok"
	errorClass := ""
	defer func() {
		recordHTTPProxyStage("total", result, errorClass, time.Since(started))
	}()
	if stream == nil || strings.TrimSpace(stream.TargetID()) == "" || stream.Port() <= 0 || stream.Port() > 65535 {
		result = "error"
		errorClass = "invalid_argument"
		return errord.ErrInvalidArgument
	}
	ctx := stream.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	lookupStart := time.Now()
	ip, err := c.ContainerIP(stream.TargetID())
	if err != nil {
		result = "error"
		errorClass = "container_ip_lookup"
		recordHTTPProxyStage("container_ip_lookup", "error", errorClass, time.Since(lookupStart))
		return err
	}
	recordHTTPProxyStage("container_ip_lookup", "ok", "", time.Since(lookupStart))

	var bodyReader io.Reader
	var bodyWriter *io.PipeWriter
	recvErrCh := make(chan error, 1)
	if stream.HasBody() {
		reader, writer := io.Pipe()
		bodyReader = reader
		bodyWriter = writer
	} else {
		recvErrCh <- nil
	}
	defer func() {
		if retErr != nil {
			closeHTTPProxyBodyWriter(bodyWriter, retErr)
		}
	}()

	rawPath := stream.Path()
	if rawPath == "" {
		rawPath = "/"
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	target := "http://" + net.JoinHostPort(ip, strconv.Itoa(int(stream.Port()))) + rawPath
	if query := stream.Query(); query != "" {
		target += "?" + query
	}
	req, err := http.NewRequestWithContext(httpProxyTraceContext(ctx), stream.Method(), target, bodyReader)
	if err != nil {
		result = "error"
		errorClass = "invalid_request"
		closeHTTPProxyBodyWriter(bodyWriter, err)
		return err
	}
	if stream.HasBody() {
		req.ContentLength = stream.ContentLength()
		go recvHTTPProxyBody(stream, bodyWriter, recvErrCh)
	}
	req.Header = stream.Header().Clone()

	roundTripStart := time.Now()
	resp, err := c.httpProxyTransport(stream.TargetID(), stream.Port()).RoundTrip(req)
	if err != nil {
		result = "error"
		errorClass = "upstream"
		closeHTTPProxyBodyWriter(bodyWriter, err)
		recordHTTPProxyStage("upstream_round_trip", "error", errorClass, time.Since(roundTripStart))
		return err
	}
	recordHTTPProxyStage("upstream_round_trip", "ok", "", time.Since(roundTripStart))
	defer resp.Body.Close()
	if err := stream.SendHead(resp.StatusCode, resp.Header); err != nil {
		result = "error"
		errorClass = "send_head"
		return err
	}
	pumpStart := time.Now()
	if err := sendHTTPProxyResponseBody(stream, resp.Body); err != nil {
		result = "error"
		errorClass = "stream_pump"
		recordHTTPProxyStage("stream_pump", "error", errorClass, time.Since(pumpStart))
		return err
	}
	if err := stream.SendTrailers(resp.Trailer); err != nil {
		result = "error"
		errorClass = "stream_pump"
		recordHTTPProxyStage("stream_pump", "error", errorClass, time.Since(pumpStart))
		return err
	}
	if err := <-recvErrCh; err != nil {
		result = "error"
		errorClass = "stream_pump"
		recordHTTPProxyStage("stream_pump", "error", errorClass, time.Since(pumpStart))
		return err
	}
	recordHTTPProxyStage("stream_pump", "ok", "", time.Since(pumpStart))
	return nil
}

func (c *Coordinator) ProbePort(ctx context.Context, targetID string, port int32) error {
	if c == nil || strings.TrimSpace(targetID) == "" || port <= 0 || port > 65535 {
		return errord.ErrInvalidArgument
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ip, err := c.ContainerIP(targetID)
	if err != nil {
		return err
	}
	conn, err := c.connectPort(ctx, net.JoinHostPort(ip, strconv.Itoa(int(port))))
	if err != nil {
		return fmt.Errorf("connect allocation port: %w", err)
	}
	return conn.Close()
}

func (c *Coordinator) httpProxyTransport(targetID string, port int32) *http.Transport {
	key := httpProxyTransportKey(targetID, port)
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	if transport := c.proxies[key]; transport != nil {
		return transport
	}
	transport := &http.Transport{
		MaxIdleConns:        512,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return c.connectPort(ctx, address)
		},
	}
	c.proxies[key] = transport
	return transport
}

func (c *Coordinator) CloseHTTPProxyTransports(targetID string) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return
	}
	c.proxyMu.Lock()
	var transports []*http.Transport
	prefix := targetID + "\x00"
	for key, transport := range c.proxies {
		if strings.HasPrefix(key, prefix) {
			transports = append(transports, transport)
			delete(c.proxies, key)
		}
	}
	c.proxyMu.Unlock()
	for _, transport := range transports {
		transport.CloseIdleConnections()
	}
}

func httpProxyTransportKey(targetID string, port int32) string {
	return strings.TrimSpace(targetID) + "\x00" + strconv.Itoa(int(port))
}

func httpProxyTraceContext(ctx context.Context) context.Context {
	var connectStart time.Time
	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) {
			connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, err error) {
			if connectStart.IsZero() {
				return
			}
			result := "ok"
			errorClass := ""
			if err != nil {
				result = "error"
				errorClass = "connect_container_port"
			}
			recordHTTPProxyStage("connect_container_port", result, errorClass, time.Since(connectStart))
		},
	}
	return httptrace.WithClientTrace(ctx, trace)
}

func recvHTTPProxyBody(stream HTTPProxyStream, body *io.PipeWriter, done chan<- error) {
	var err error
	defer func() {
		if err != nil {
			_ = body.CloseWithError(err)
		} else {
			_ = body.Close()
		}
		done <- err
	}()
	for {
		chunk, recvErr := stream.RecvBody()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				return
			}
			err = recvErr
			return
		}
		if len(chunk) == 0 {
			continue
		}
		if _, err = body.Write(chunk); err != nil {
			return
		}
	}
}

func closeHTTPProxyBodyWriter(body *io.PipeWriter, err error) {
	if body != nil {
		_ = body.CloseWithError(err)
	}
}

func sendHTTPProxyResponseBody(stream HTTPProxyStream, body io.Reader) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if sendErr := stream.SendBody(append([]byte(nil), buf[:n]...)); sendErr != nil {
				return sendErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func recordHTTPProxyStage(stage, result, errorClass string, duration time.Duration) {
	if result == "" {
		result = "unknown"
	}
	if errorClass == "" {
		errorClass = "none"
	}
	metrics.RecordHTTPProxyStageDuration(stage, result, errorClass, duration.Seconds())
}

func (c *Coordinator) connectPort(ctx context.Context, address string) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	connectCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok && c.connectTimeout > 0 {
		connectCtx, cancel = context.WithTimeout(ctx, c.connectTimeout)
	}
	defer cancel()

	for {
		conn, dialErr := c.dialContext(connectCtx, "tcp", address)
		if dialErr == nil {
			return conn, nil
		}
		if connectCtx.Err() != nil {
			return nil, dialErr
		}
		if !retryablePortConnectError(dialErr) {
			return nil, dialErr
		}
		if err := sleepContext(connectCtx, c.connectRetryDelay); err != nil {
			return nil, dialErr
		}
	}
}

func retryablePortConnectError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "connection refused") ||
		strings.Contains(text, "i/o timeout") ||
		strings.Contains(text, "no route to host") ||
		strings.Contains(text, "network is unreachable")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
