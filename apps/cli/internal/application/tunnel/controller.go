package tunnel

import (
	"context"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

type TunnelClient interface {
	CreateTunnelSession(context.Context, *tunnelv1.CreateTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.CreateTunnelSessionResponse, error)
	GetTunnelSession(context.Context, *tunnelv1.GetTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.GetTunnelSessionResponse, error)
	ListTunnelSessions(context.Context, *tunnelv1.ListTunnelSessionsRequest, ...grpc.CallOption) (*tunnelv1.ListTunnelSessionsResponse, error)
	RenewTunnelSession(context.Context, *tunnelv1.RenewTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.RenewTunnelSessionResponse, error)
	RevokeTunnelSession(context.Context, *tunnelv1.RevokeTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.RevokeTunnelSessionResponse, error)
	ListTunnelSessionEvents(context.Context, *tunnelv1.ListTunnelSessionEventsRequest, ...grpc.CallOption) (*tunnelv1.ListTunnelSessionEventsResponse, error)
	InspectTunnelSession(context.Context, *tunnelv1.InspectTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.InspectTunnelSessionResponse, error)
}

type Control struct {
	client TunnelClient
}

type ListParams struct {
	Namespace       string
	AllocationID    string
	NodeID          string
	IncludeTerminal bool
}

type CreateParams struct {
	AllocationID string
	RemotePort   *int32
	LocalTarget  string
	TTL          time.Duration
	WaitReady    bool
	ReadyTimeout time.Duration
}

func New(client TunnelClient) Control {
	return Control{client: client}
}

func (c Control) Create(ctx context.Context, params CreateParams) (*tunnelv1.CreateTunnelSessionResponse, error) {
	return c.client.CreateTunnelSession(ctx, &tunnelv1.CreateTunnelSessionRequest{
		AllocationID: params.AllocationID,
		RemotePort:   params.RemotePort,
		LocalTarget:  params.LocalTarget,
		Ttl:          durationpb.New(params.TTL),
		WaitReady:    params.WaitReady,
		ReadyTimeout: durationpb.New(params.ReadyTimeout),
	})
}

func (c Control) Get(ctx context.Context, sessionID string) (*tunnelv1.GetTunnelSessionResponse, error) {
	return c.client.GetTunnelSession(ctx, &tunnelv1.GetTunnelSessionRequest{SessionID: sessionID})
}

func (c Control) List(ctx context.Context, params ListParams) (*tunnelv1.ListTunnelSessionsResponse, error) {
	return c.client.ListTunnelSessions(ctx, &tunnelv1.ListTunnelSessionsRequest{
		Namespace:       params.Namespace,
		AllocationID:    params.AllocationID,
		NodeID:          params.NodeID,
		IncludeTerminal: params.IncludeTerminal,
	})
}

func (c Control) Revoke(ctx context.Context, sessionID, reason string) (*tunnelv1.RevokeTunnelSessionResponse, error) {
	return c.client.RevokeTunnelSession(ctx, &tunnelv1.RevokeTunnelSessionRequest{
		SessionID: sessionID,
		Reason:    reason,
	})
}

func (c Control) Events(ctx context.Context, sessionID string, limit int32) (*tunnelv1.ListTunnelSessionEventsResponse, error) {
	return c.client.ListTunnelSessionEvents(ctx, &tunnelv1.ListTunnelSessionEventsRequest{
		SessionID: sessionID,
		Limit:     limit,
	})
}

func (c Control) Inspect(ctx context.Context, sessionID string, eventLimit int32) (*tunnelv1.InspectTunnelSessionResponse, error) {
	return c.client.InspectTunnelSession(ctx, &tunnelv1.InspectTunnelSessionRequest{
		SessionID:  sessionID,
		EventLimit: eventLimit,
	})
}
