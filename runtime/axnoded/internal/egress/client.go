package egress

import (
	"context"
	"fmt"
	"net"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const DefaultSocket = "/run/egressd/egressd.sock"

// Manager is the fail-closed node-local policy lifecycle used by allocation.
// An implementation must not report Prepare success until its dataplane is
// active for the exact allocation attempt and sandbox IP.
type Manager interface {
	Prepare(context.Context, string, int64, string, *commonv1.NetworkEgressPolicy, int64, []string) (*runtimeegressv1.PreparedEgressPolicy, error)
	Delete(context.Context, string, int64) error
	Get(context.Context, string, int64) (*runtimeegressv1.PreparedEgressPolicy, error)
	Reconcile(context.Context, []*runtimeegressv1.ActiveEgressPolicy) (*runtimeegressv1.ReconcilePoliciesResponse, error)
	Health(context.Context) (*runtimeegressv1.EgressManagerHealth, error)
}

type Client struct {
	conn   *grpc.ClientConn
	client runtimeegressv1.RuntimeEgressServiceClient
}

func Dial(ctx context.Context, socket string) (*Client, error) {
	_ = ctx
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return nil, fmt.Errorf("egressd socket is required")
	}
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}
	// Connectivity is deliberately checked by Health/Prepare rather than at
	// process construction. Nodes without egressd must continue serving legacy
	// unrestricted sandboxes while advertising policy capabilities unavailable.
	conn, err := grpc.NewClient("unix:"+socket, grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial egressd %s: %w", socket, err)
	}
	return &Client{conn: conn, client: runtimeegressv1.NewRuntimeEgressServiceClient(conn)}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Prepare(ctx context.Context, allocationID string, attempt int64, sandboxIP string, policy *commonv1.NetworkEgressPolicy, revision int64, upstreams []string) (*runtimeegressv1.PreparedEgressPolicy, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("egressd client is not configured")
	}
	resp, err := c.client.PreparePolicy(ctx, &runtimeegressv1.PreparePolicyRequest{AllocationID: allocationID, Attempt: attempt, SandboxIp: sandboxIP, Policy: policy, ExecutionRevision: revision, UpstreamNameservers: append([]string(nil), upstreams...)})
	if err != nil {
		return nil, err
	}
	if resp.GetPolicy() == nil {
		return nil, fmt.Errorf("egressd returned an empty prepared policy")
	}
	return resp.GetPolicy(), nil
}

func (c *Client) Delete(ctx context.Context, allocationID string, attempt int64) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("egressd client is not configured")
	}
	_, err := c.client.DeletePolicy(ctx, &runtimeegressv1.DeletePolicyRequest{AllocationID: allocationID, Attempt: attempt})
	return err
}

func (c *Client) Get(ctx context.Context, allocationID string, attempt int64) (*runtimeegressv1.PreparedEgressPolicy, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("egressd client is not configured")
	}
	resp, err := c.client.GetPolicy(ctx, &runtimeegressv1.GetPolicyRequest{AllocationID: allocationID, Attempt: attempt})
	if err != nil {
		return nil, err
	}
	return resp.GetPolicy(), nil
}

func (c *Client) Reconcile(ctx context.Context, active []*runtimeegressv1.ActiveEgressPolicy) (*runtimeegressv1.ReconcilePoliciesResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("egressd client is not configured")
	}
	return c.client.ReconcilePolicies(ctx, &runtimeegressv1.ReconcilePoliciesRequest{ActivePolicies: active})
}

func (c *Client) Health(ctx context.Context) (*runtimeegressv1.EgressManagerHealth, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("egressd client is not configured")
	}
	resp, err := c.client.GetEgressManagerHealth(ctx, &runtimeegressv1.EgressManagerHealthRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetHealth(), nil
}
