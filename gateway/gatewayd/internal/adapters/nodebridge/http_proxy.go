package nodebridge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const proxyHTTPChunkSize = 32 * 1024

func (d *Dialer) ProxyHTTP(ctx context.Context, spec nodekernel.HTTPProxySpec) (*http.Response, error) {
	client, err := d.client(ctx, spec.NodeTarget)
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := proxyHTTPStreamContext(ctx, spec.Timeout)
	stream, err := client.ProxyHTTP(streamCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := stream.Send(&nodesandboxv1.ProxyHTTPRequest{
		Payload: &nodesandboxv1.ProxyHTTPRequest_Open{Open: &nodesandboxv1.ProxyHTTPOpen{
			AllocationID:        spec.AllocationID,
			Attempt:             spec.Attempt,
			ExecutionLeaseToken: spec.Token,
			Port:                spec.Port,
			Method:              spec.Method,
			Path:                spec.Path,
			Query:               spec.Query,
			Headers:             proxyHeadersFromHTTP(spec.Header),
			HasBody:             spec.HasBody,
			ContentLength:       spec.ContentLength,
		}},
	}); err != nil {
		cancel()
		return nil, err
	}
	header, err := stream.Header()
	if err != nil {
		cancel()
		return nil, err
	}
	if !nodekernel.ExecutionLeaseAccepted(header) {
		if _, err := stream.Recv(); err != nil {
			cancel()
			return nil, err
		}
		cancel()
		return nil, status.Error(codes.FailedPrecondition, "node did not acknowledge execution lease before proxy response")
	}
	sendErrCh := make(chan error, 1)
	go sendProxyHTTPRequestBody(stream, spec.Body, spec.HasBody, sendErrCh)

	head, err := recvProxyHTTPHead(stream)
	if err != nil {
		cancel()
		return nil, err
	}
	bodyReader, bodyWriter := io.Pipe()
	resp := &http.Response{
		StatusCode: int(head.GetStatusCode()),
		Status:     fmt.Sprintf("%d %s", head.GetStatusCode(), http.StatusText(int(head.GetStatusCode()))),
		Header:     httpHeadersFromProxy(head.GetHeaders()),
		Body:       cancelOnCloseReadCloser{ReadCloser: bodyReader, cancel: cancel},
		Trailer:    http.Header{},
	}
	go recvProxyHTTPResponseBody(stream, bodyWriter, resp.Trailer, sendErrCh, cancel)
	return resp, nil
}

func proxyHTTPStreamContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func sendProxyHTTPRequestBody(stream nodesandboxv1.NodeSandbox_ProxyHTTPClient, body io.Reader, hasBody bool, done chan<- error) {
	var err error
	defer func() {
		if hasBody && err == nil {
			err = stream.Send(&nodesandboxv1.ProxyHTTPRequest{
				Payload: &nodesandboxv1.ProxyHTTPRequest_CloseBody{CloseBody: true},
			})
		}
		if err == nil {
			err = stream.CloseSend()
		}
		done <- err
	}()
	if !hasBody || body == nil {
		return
	}
	buf := make([]byte, proxyHTTPChunkSize)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if err = stream.Send(&nodesandboxv1.ProxyHTTPRequest{
				Payload: &nodesandboxv1.ProxyHTTPRequest_Body{Body: chunk},
			}); err != nil {
				return
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				err = readErr
			}
			return
		}
	}
}

func recvProxyHTTPHead(stream nodesandboxv1.NodeSandbox_ProxyHTTPClient) (*nodesandboxv1.ProxyHTTPResponseHead, error) {
	for {
		resp, err := stream.Recv()
		if err != nil {
			return nil, err
		}
		switch payload := resp.GetPayload().(type) {
		case *nodesandboxv1.ProxyHTTPResponse_Head:
			return payload.Head, nil
		case *nodesandboxv1.ProxyHTTPResponse_Error:
			return nil, fmt.Errorf("proxy http upstream error: %s", payload.Error)
		default:
			return nil, fmt.Errorf("proxy http response head is required")
		}
	}
}

func recvProxyHTTPResponseBody(stream nodesandboxv1.NodeSandbox_ProxyHTTPClient, body *io.PipeWriter, trailer http.Header, sendErrCh <-chan error, cancel context.CancelFunc) {
	defer func() {
		cancel()
		if err := <-sendErrCh; err != nil {
			_ = body.CloseWithError(err)
		}
	}()
	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				_ = body.Close()
				return
			}
			_ = body.CloseWithError(err)
			return
		}
		switch payload := resp.GetPayload().(type) {
		case *nodesandboxv1.ProxyHTTPResponse_Body:
			if len(payload.Body) > 0 {
				if _, err := body.Write(payload.Body); err != nil {
					_ = body.CloseWithError(err)
					return
				}
			}
		case *nodesandboxv1.ProxyHTTPResponse_Trailers:
			for key, values := range httpHeadersFromProxy(payload.Trailers.GetHeaders()) {
				for _, value := range values {
					trailer.Add(key, value)
				}
			}
		case *nodesandboxv1.ProxyHTTPResponse_Error:
			_ = body.CloseWithError(fmt.Errorf("proxy http upstream error: %s", payload.Error))
			return
		}
	}
}

func proxyHeadersFromHTTP(header http.Header) []*nodesandboxv1.ProxyHTTPHeader {
	out := make([]*nodesandboxv1.ProxyHTTPHeader, 0, len(header))
	for key, values := range header {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		for _, value := range values {
			out = append(out, &nodesandboxv1.ProxyHTTPHeader{Key: key, Value: value})
		}
	}
	return out
}

func httpHeadersFromProxy(headers []*nodesandboxv1.ProxyHTTPHeader) http.Header {
	out := make(http.Header)
	for _, header := range headers {
		key := strings.TrimSpace(header.GetKey())
		if key == "" {
			continue
		}
		out.Add(key, header.GetValue())
	}
	return out
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnCloseReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
