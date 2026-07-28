package nodebridge

import (
	"context"
	"io"
	"net/http"
	"time"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type HTTPProxySpec struct {
	NodeTarget    string
	AllocationID  string
	Attempt       int64
	Token         string
	Port          int32
	Method        string
	Path          string
	Query         string
	Header        http.Header
	Body          io.Reader
	HasBody       bool
	ContentLength int64
	Timeout       time.Duration
}

type HTTPProxyer interface {
	ProxyHTTP(ctx context.Context, spec HTTPProxySpec) (*http.Response, error)
}

type ExecStreamer interface {
	ExecStream(ctx context.Context, target string) (nodesandboxv1.NodeSandbox_ExecStreamClient, error)
}
