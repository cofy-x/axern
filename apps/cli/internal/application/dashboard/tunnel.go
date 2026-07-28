package dashboard

import (
	"context"
	"fmt"
	"strings"

	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
)

func (c Control) Tunnels(ctx context.Context, params TunnelListParams) ([]TunnelDTO, error) {
	resp, err := c.tunnels.List(ctx, apptunnel.ListParams{
		AllocationID:    strings.TrimSpace(params.AllocationID),
		NodeID:          strings.TrimSpace(params.NodeID),
		IncludeTerminal: params.IncludeTerminal,
	})
	if err != nil {
		return nil, err
	}
	out := make([]TunnelDTO, 0, len(resp.GetSessions()))
	for _, session := range resp.GetSessions() {
		out = append(out, NewTunnelDTO(session))
	}
	return out, nil
}

func (c Control) TunnelDetail(ctx context.Context, sessionID string) (TunnelDetail, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return TunnelDetail{}, fmt.Errorf("session id is required")
	}
	resp, err := c.tunnels.Inspect(ctx, sessionID, DefaultEventLimit)
	if err != nil {
		return TunnelDetail{}, err
	}
	events := make([]TunnelEvent, 0, len(resp.GetEvents()))
	for _, event := range resp.GetEvents() {
		events = append(events, NewTunnelEvent(event))
	}
	return TunnelDetail{Session: ptr(NewTunnelDTO(resp.GetSession())), Events: events}, nil
}

func (c Control) TunnelEvents(ctx context.Context, sessionID string, limit int32) ([]TunnelEvent, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	resp, err := c.tunnels.Events(ctx, sessionID, NormalizeLimit(limit))
	if err != nil {
		return nil, err
	}
	out := make([]TunnelEvent, 0, len(resp.GetEvents()))
	for _, event := range resp.GetEvents() {
		out = append(out, NewTunnelEvent(event))
	}
	return out, nil
}

func (c Control) TunnelDoctorWithService(ctx context.Context, params apptunnel.DoctorParams, serviceClient apptunnel.ServiceClient) (apptunnel.DoctorReport, error) {
	params.ServiceClient = serviceClient
	return c.tunnels.Doctor(ctx, params)
}
