package nodeclient

import (
	"bytes"
	"context"

	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func (c *Client) Stat(ctx context.Context, path string) (*filev1.SandboxFileInfo, error) {
	response, err := c.nodes.StatFile(ctx, &nodesandboxv1.StatFileRequest{
		AllocationID: c.allocationID,
		Path:         path,
	})
	if err != nil {
		return nil, err
	}
	return response.GetInfo(), nil
}

func (c *Client) ListDir(ctx context.Context, path string) ([]*filev1.SandboxFileInfo, error) {
	response, err := c.nodes.ListDir(ctx, &nodesandboxv1.ListDirRequest{
		AllocationID: c.allocationID,
		Path:         path,
	})
	if err != nil {
		return nil, err
	}
	return response.GetEntries(), nil
}

func (c *Client) ReadFile(ctx context.Context, path string) ([]byte, error) {
	response, err := c.nodes.ReadFile(ctx, &nodesandboxv1.ReadFileRequest{
		AllocationID: c.allocationID,
		Path:         path,
	})
	if err != nil {
		return nil, err
	}
	return bytes.Clone(response.GetData()), nil
}

func (c *Client) WriteFile(ctx context.Context, path string, data []byte, createParents bool) error {
	_, err := c.nodes.WriteFile(ctx, &nodesandboxv1.WriteFileRequest{
		AllocationID:  c.allocationID,
		Path:          path,
		Data:          bytes.Clone(data),
		CreateParents: createParents,
	})
	return err
}

func (c *Client) MaterializeTaskAssets(ctx context.Context, sourcePath, target string, kind nodesandboxv1.TaskAssetKind) (int64, error) {
	response, err := c.nodes.MaterializeTaskAssets(ctx, &nodesandboxv1.MaterializeTaskAssetsRequest{AllocationID: c.allocationID, SourcePath: sourcePath, Target: target, Kind: kind})
	if err != nil {
		return 0, err
	}
	return response.GetDurationMs(), nil
}

func (c *Client) Exists(ctx context.Context, path string) (bool, error) {
	response, err := c.nodes.Exists(ctx, &nodesandboxv1.ExistsRequest{
		AllocationID: c.allocationID,
		Path:         path,
	})
	if err != nil {
		return false, err
	}
	return response.GetExists(), nil
}

func (c *Client) Mkdir(ctx context.Context, path string, parents bool) error {
	_, err := c.nodes.Mkdir(ctx, &nodesandboxv1.MkdirRequest{
		AllocationID: c.allocationID,
		Path:         path,
		Parents:      parents,
	})
	return err
}

func (c *Client) Remove(ctx context.Context, path string, recursive, force bool) error {
	_, err := c.nodes.Remove(ctx, &nodesandboxv1.RemoveRequest{
		AllocationID: c.allocationID,
		Path:         path,
		Recursive:    recursive,
		Force:        force,
	})
	return err
}

func (c *Client) Copy(ctx context.Context, srcPath, dstPath string, recursive, overwrite bool) error {
	_, err := c.nodes.Copy(ctx, &nodesandboxv1.CopyRequest{
		AllocationID: c.allocationID,
		SrcPath:      srcPath,
		DstPath:      dstPath,
		Recursive:    recursive,
		Overwrite:    overwrite,
	})
	return err
}

func (c *Client) Move(ctx context.Context, srcPath, dstPath string, overwrite bool) error {
	_, err := c.nodes.Move(ctx, &nodesandboxv1.MoveRequest{
		AllocationID: c.allocationID,
		SrcPath:      srcPath,
		DstPath:      dstPath,
		Overwrite:    overwrite,
	})
	return err
}

func (c *Client) Chmod(ctx context.Context, path string, mode uint32, recursive bool) error {
	_, err := c.nodes.Chmod(ctx, &nodesandboxv1.ChmodRequest{
		AllocationID: c.allocationID,
		Path:         path,
		Mode:         mode,
		Recursive:    recursive,
	})
	return err
}

func (c *Client) Touch(ctx context.Context, path string, create bool, mtimeNS int64) error {
	_, err := c.nodes.Touch(ctx, &nodesandboxv1.TouchRequest{
		AllocationID: c.allocationID,
		Path:         path,
		Create:       create,
		MtimeNs:      mtimeNS,
	})
	return err
}
