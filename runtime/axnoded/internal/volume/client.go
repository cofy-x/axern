package volume

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	socket string
	conn   *grpc.ClientConn
	client runtimevolumev1.RuntimeVolumeServiceClient
}

type Publisher interface {
	PublishAll(context.Context, string, string, []*privatestoragev1.ResolvedNodeVolume) ([]*privatestoragev1.PublishedNodeVolume, error)
	ListPublishedVolumes(context.Context, string) ([]*privatestoragev1.PublishedNodeVolume, error)
	UnpublishAllocation(context.Context, string) ([]*privatestoragev1.VolumeReleaseObservation, error)
	ReconcileActiveAllocations(context.Context, []string) (*runtimevolumev1.ReconcileVolumesResponse, error)
	Health(context.Context) (*runtimevolumev1.VolumeManagerHealth, error)
	DeleteVolume(context.Context, string, storagev1.VolumeBackend, string) error
}

func (c *Client) DeleteVolume(ctx context.Context, claimID string, backend storagev1.VolumeBackend, backendHandle string) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("volumed client is not configured")
	}
	_, err := c.client.DeleteVolume(ctx, &runtimevolumev1.DeleteVolumeRequest{
		ClaimID: claimID, Backend: backend, BackendHandle: backendHandle,
	})
	return err
}

var _ Publisher = (*Client)(nil)

func Dial(ctx context.Context, socket string) (*Client, error) {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return nil, fmt.Errorf("volumed socket is required")
	}
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := grpcclient.NewReadyClient(dialCtx, "unix:"+socket, grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial volumed %s: %w", socket, err)
	}
	return &Client{
		socket: socket,
		conn:   conn,
		client: runtimevolumev1.NewRuntimeVolumeServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) PublishAll(ctx context.Context, allocationID, runtimeClass string, volumes []*privatestoragev1.ResolvedNodeVolume) ([]*privatestoragev1.PublishedNodeVolume, error) {
	if len(volumes) == 0 {
		return nil, nil
	}
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("volumed client is not configured")
	}
	published := make([]*privatestoragev1.PublishedNodeVolume, 0, len(volumes))
	for _, volume := range volumes {
		if volume == nil {
			_, _ = c.UnpublishAllocation(ctx, allocationID)
			return nil, fmt.Errorf("resolved node volume is required")
		}
		resp, err := c.client.PublishVolume(ctx, &runtimevolumev1.PublishVolumeRequest{
			AllocationID: allocationID,
			RuntimeClass: runtimeClass,
			Volume:       volume,
		})
		if err != nil {
			_, _ = c.UnpublishAllocation(ctx, allocationID)
			return nil, err
		}
		if resp.GetVolume() == nil {
			_, _ = c.UnpublishAllocation(ctx, allocationID)
			return nil, fmt.Errorf("volumed returned an empty published volume")
		}
		published = append(published, publishedNodeVolume(resp.GetVolume()))
	}
	return published, nil
}

func (c *Client) ListPublishedVolumes(ctx context.Context, allocationID string) ([]*privatestoragev1.PublishedNodeVolume, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	resp, err := c.client.ListPublishedVolumes(ctx, &runtimevolumev1.ListPublishedVolumesRequest{
		AllocationID: allocationID,
	})
	if err != nil {
		return nil, err
	}
	return publishedNodeVolumes(resp.GetVolumes()), nil
}

func (c *Client) UnpublishAllocation(ctx context.Context, allocationID string) ([]*privatestoragev1.VolumeReleaseObservation, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	resp, err := c.client.UnpublishVolume(ctx, &runtimevolumev1.UnpublishVolumeRequest{
		AllocationID: allocationID,
	})
	if err != nil {
		return nil, err
	}
	return volumeReleaseObservations(resp.GetVolumes()), nil
}

func (c *Client) ReconcileActiveAllocations(ctx context.Context, allocationIDs []string) (*runtimevolumev1.ReconcileVolumesResponse, error) {
	if c == nil || c.client == nil {
		return &runtimevolumev1.ReconcileVolumesResponse{}, nil
	}
	active := make([]string, 0, len(allocationIDs))
	seen := make(map[string]struct{}, len(allocationIDs))
	for _, allocationID := range allocationIDs {
		allocationID = strings.TrimSpace(allocationID)
		if allocationID == "" {
			continue
		}
		if _, ok := seen[allocationID]; ok {
			continue
		}
		seen[allocationID] = struct{}{}
		active = append(active, allocationID)
	}
	return c.client.ReconcileVolumes(ctx, &runtimevolumev1.ReconcileVolumesRequest{ActiveAllocationIds: active})
}

func (c *Client) Health(ctx context.Context) (*runtimevolumev1.VolumeManagerHealth, error) {
	if c == nil || c.client == nil {
		return &runtimevolumev1.VolumeManagerHealth{Status: runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_DISABLED}, nil
	}
	resp, err := c.client.GetVolumeManagerHealth(ctx, &runtimevolumev1.VolumeManagerHealthRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetHealth(), nil
}

func publishedNodeVolume(in *runtimevolumev1.PublishedVolume) *privatestoragev1.PublishedNodeVolume {
	if in == nil {
		return nil
	}
	return &privatestoragev1.PublishedNodeVolume{
		ClaimID:   in.GetClaimID(),
		BindingID: in.GetBindingID(),
		Backend:   in.GetBackend(),
		HostPath:  in.GetHostPath(),
		Target:    in.GetTarget(),
		Readonly:  in.GetReadonly(),
		Options:   append([]string(nil), in.GetOptions()...),
	}
}

func publishedNodeVolumes(in []*runtimevolumev1.PublishedVolume) []*privatestoragev1.PublishedNodeVolume {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatestoragev1.PublishedNodeVolume, 0, len(in))
	for _, volume := range in {
		if volume == nil {
			continue
		}
		out = append(out, publishedNodeVolume(volume))
	}
	return out
}

func volumeReleaseObservations(volumes []*runtimevolumev1.PublishedVolume) []*privatestoragev1.VolumeReleaseObservation {
	if len(volumes) == 0 {
		return nil
	}
	out := make([]*privatestoragev1.VolumeReleaseObservation, 0, len(volumes))
	for _, volume := range volumes {
		if volume == nil {
			continue
		}
		bindingID := strings.TrimSpace(volume.GetBindingID())
		if bindingID == "" {
			continue
		}
		out = append(out, &privatestoragev1.VolumeReleaseObservation{
			BindingID: bindingID,
			Status:    storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
		})
	}
	return out
}
