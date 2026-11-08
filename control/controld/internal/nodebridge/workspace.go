package nodebridge

import (
	"context"

	privatenodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
)

func (c *GRPCClient) ProvisionWorkspace(ctx context.Context, target string, req *privatenodev1.ProvisionWorkspaceRequest) (*privatenodev1.ProvisionWorkspaceResponse, error) {
	client, conn, err := c.clientConn(ctx, target, req.GetSpec().GetNodeID())
	if err != nil {
		return nil, err
	}
	resp, err := client.ProvisionWorkspace(ctx, req)
	c.discardRecoverableConn(nodeEndpoint{target, req.GetSpec().GetNodeID()}, conn, err)
	return resp, err
}
func (c *GRPCClient) InspectWorkspace(ctx context.Context, target string, req *privatenodev1.InspectWorkspaceRequest) (*privatenodev1.InspectWorkspaceResponse, error) {
	client, conn, err := c.clientConn(ctx, target, req.GetSpec().GetNodeID())
	if err != nil {
		return nil, err
	}
	resp, err := client.InspectWorkspace(ctx, req)
	c.discardRecoverableConn(nodeEndpoint{target, req.GetSpec().GetNodeID()}, conn, err)
	return resp, err
}
func (c *GRPCClient) DeleteWorkspace(ctx context.Context, target string, req *privatenodev1.DeleteWorkspaceRequest) (*privatenodev1.DeleteWorkspaceResponse, error) {
	client, conn, err := c.clientConn(ctx, target, req.GetSpec().GetNodeID())
	if err != nil {
		return nil, err
	}
	resp, err := client.DeleteWorkspace(ctx, req)
	c.discardRecoverableConn(nodeEndpoint{target, req.GetSpec().GetNodeID()}, conn, err)
	return resp, err
}
