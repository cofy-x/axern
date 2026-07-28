package api

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) ProxyHTTP(stream nodesandboxv1.NodeSandbox_ProxyHTTPServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "initial open payload is required")
	}
	target, err := s.validateDirectAuth(stream.Context(), open.GetAllocationID(), open.GetAttempt(), open.GetExecutionLeaseToken())
	if err != nil {
		return err
	}
	if open.GetPort() <= 0 || open.GetPort() > 65535 {
		return grpcstatus.Error(codes.InvalidArgument, "port must be between 1 and 65535")
	}
	method := strings.TrimSpace(open.GetMethod())
	if method == "" {
		return grpcstatus.Error(codes.InvalidArgument, "method is required")
	}
	path := open.GetPath()
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		return grpcstatus.Error(codes.InvalidArgument, "path must start with /")
	}
	// Acknowledge authentication before the gateway starts consuming the HTTP
	// request body. Lease refresh can then retry only work that the node has not
	// forwarded to the allocation.
	if err := acknowledgeExecutionLease(stream); err != nil {
		return err
	}
	return s.svc.ProxyHTTP(&proxyHTTPAdapter{
		stream:   stream,
		targetID: target.targetID,
		port:     open.GetPort(),
		method:   method,
		path:     path,
		query:    open.GetQuery(),
		header:   httpHeadersFromProxy(open.GetHeaders()),
		hasBody:  open.GetHasBody(),
		length:   open.GetContentLength(),
	})
}

type proxyHTTPAdapter struct {
	stream   nodesandboxv1.NodeSandbox_ProxyHTTPServer
	targetID string
	port     int32
	method   string
	path     string
	query    string
	header   http.Header
	hasBody  bool
	length   int64
}

func (a *proxyHTTPAdapter) TargetID() string         { return a.targetID }
func (a *proxyHTTPAdapter) Port() int32              { return a.port }
func (a *proxyHTTPAdapter) Method() string           { return a.method }
func (a *proxyHTTPAdapter) Path() string             { return a.path }
func (a *proxyHTTPAdapter) Query() string            { return a.query }
func (a *proxyHTTPAdapter) Header() http.Header      { return a.header }
func (a *proxyHTTPAdapter) HasBody() bool            { return a.hasBody }
func (a *proxyHTTPAdapter) ContentLength() int64     { return a.length }
func (a *proxyHTTPAdapter) Context() context.Context { return a.stream.Context() }

func (a *proxyHTTPAdapter) RecvBody() ([]byte, error) {
	req, err := a.stream.Recv()
	if err != nil {
		return nil, err
	}
	switch payload := req.GetPayload().(type) {
	case *nodesandboxv1.ProxyHTTPRequest_Body:
		return payload.Body, nil
	case *nodesandboxv1.ProxyHTTPRequest_CloseBody:
		if payload.CloseBody {
			return nil, io.EOF
		}
		return nil, nil
	case *nodesandboxv1.ProxyHTTPRequest_Open:
		return nil, grpcstatus.Error(codes.InvalidArgument, "open payload is only valid as the first proxy-http frame")
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "unsupported proxy-http payload")
	}
}

func (a *proxyHTTPAdapter) SendHead(statusCode int, header http.Header) error {
	return a.stream.Send(&nodesandboxv1.ProxyHTTPResponse{
		Payload: &nodesandboxv1.ProxyHTTPResponse_Head{Head: &nodesandboxv1.ProxyHTTPResponseHead{
			StatusCode: int32(statusCode),
			Headers:    proxyHeadersFromHTTP(header),
		}},
	})
}

func (a *proxyHTTPAdapter) SendBody(data []byte) error {
	return a.stream.Send(&nodesandboxv1.ProxyHTTPResponse{
		Payload: &nodesandboxv1.ProxyHTTPResponse_Body{Body: data},
	})
}

func (a *proxyHTTPAdapter) SendTrailers(header http.Header) error {
	if len(header) == 0 {
		return nil
	}
	return a.stream.Send(&nodesandboxv1.ProxyHTTPResponse{
		Payload: &nodesandboxv1.ProxyHTTPResponse_Trailers{Trailers: &nodesandboxv1.ProxyHTTPTrailers{
			Headers: proxyHeadersFromHTTP(header),
		}},
	})
}

var _ service.HTTPProxyServer = (*proxyHTTPAdapter)(nil)

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
