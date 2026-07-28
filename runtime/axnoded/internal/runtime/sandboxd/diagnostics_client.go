package sandboxd

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (c *Client) Health(ctx context.Context) (wire.HealthResponse, error) {
	var response wire.HealthResponse
	err := c.getJSON(ctx, wire.PathHealth, &response)
	return response, err
}

func (c *Client) Ready(ctx context.Context) (wire.ReadyResponse, error) {
	var response wire.ReadyResponse
	err := c.getJSON(ctx, wire.PathReady, &response)
	return response, err
}

func (c *Client) Capabilities(ctx context.Context) (wire.CapabilitiesResponse, error) {
	var response wire.CapabilitiesResponse
	err := c.getJSON(ctx, wire.PathCapabilities, &response)
	return response, err
}

func (c *Client) Status(ctx context.Context) (wire.StatusResponse, error) {
	var response wire.StatusResponse
	err := c.getJSON(ctx, wire.PathStatus, &response)
	return response, err
}

func (c *Client) Diagnostics(ctx context.Context) (wire.DiagnosticsResponse, error) {
	return c.DiagnosticsFull(ctx)
}

func (c *Client) DiagnosticsSummary(ctx context.Context) (wire.DiagnosticsResponse, error) {
	var response wire.DiagnosticsResponse
	err := c.getJSON(ctx, wire.PathDiagnostics, &response)
	return response, err
}

func (c *Client) DiagnosticsFull(ctx context.Context) (wire.DiagnosticsResponse, error) {
	var response wire.DiagnosticsResponse
	err := c.getJSON(ctx, wire.PathDiagnostics+"?detail=full", &response)
	return response, err
}

func (c *Client) Probe(ctx context.Context, request wire.ProbeRequest) (wire.ProbeResponse, error) {
	var response wire.ProbeResponse
	err := c.postJSON(ctx, wire.PathProbe, request, &response)
	return response, err
}

func (c *Client) Ports(ctx context.Context) (wire.PortSnapshot, error) {
	var response wire.PortSnapshot
	err := c.getJSON(ctx, wire.PathPorts, &response)
	return response, err
}

func (c *Client) Mounts(ctx context.Context) (wire.MountSnapshot, error) {
	var response wire.MountSnapshot
	err := c.getJSON(ctx, wire.PathMounts, &response)
	return response, err
}
