package axernsdk

import (
	"context"
)

func (n *NodeSandboxClient) Stat(ctx context.Context, path string) (SandboxFileInfo, error) {
	if err := n.validate(); err != nil {
		return SandboxFileInfo{}, err
	}
	if path == "" {
		return SandboxFileInfo{}, errPathRequired()
	}
	info, err := n.rpcClient().Stat(ctx, path)
	if err != nil {
		return SandboxFileInfo{}, mapRPCError(err, "sandbox stat file", n.allocationID)
	}
	return sandboxFileInfo(info), nil
}

func (n *NodeSandboxClient) ListDir(ctx context.Context, path string) ([]SandboxFileInfo, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errPathRequired()
	}
	entries, err := n.rpcClient().ListDir(ctx, path)
	if err != nil {
		return nil, mapRPCError(err, "sandbox list directory", n.allocationID)
	}
	return sandboxFileInfos(entries), nil
}

func (n *NodeSandboxClient) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errPathRequired()
	}
	data, err := n.rpcClient().ReadFile(ctx, path)
	if err != nil {
		return nil, mapRPCError(err, "sandbox read file", n.allocationID)
	}
	return data, nil
}

func (n *NodeSandboxClient) WriteFile(ctx context.Context, path string, data []byte, options WriteFileOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if path == "" {
		return errPathRequired()
	}
	err := n.rpcClient().WriteFile(ctx, path, data, options.CreateParents)
	return mapRPCError(err, "sandbox write file", n.allocationID)
}

func (n *NodeSandboxClient) Exists(ctx context.Context, path string) (bool, error) {
	if err := n.validate(); err != nil {
		return false, err
	}
	if path == "" {
		return false, errPathRequired()
	}
	exists, err := n.rpcClient().Exists(ctx, path)
	if err != nil {
		return false, mapRPCError(err, "sandbox exists", n.allocationID)
	}
	return exists, nil
}

func (n *NodeSandboxClient) Mkdir(ctx context.Context, path string, options MkdirOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if path == "" {
		return errPathRequired()
	}
	err := n.rpcClient().Mkdir(ctx, path, options.Parents)
	return mapRPCError(err, "sandbox make directory", n.allocationID)
}

func (n *NodeSandboxClient) Remove(ctx context.Context, path string, options RemoveOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if path == "" {
		return errPathRequired()
	}
	err := n.rpcClient().Remove(ctx, path, options.Recursive, options.Force)
	return mapRPCError(err, "sandbox remove", n.allocationID)
}

func (n *NodeSandboxClient) Copy(ctx context.Context, srcPath, dstPath string, options CopyOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if srcPath == "" || dstPath == "" {
		return errSrcDstPathRequired()
	}
	err := n.rpcClient().Copy(ctx, srcPath, dstPath, options.Recursive, options.Overwrite)
	return mapRPCError(err, "sandbox copy", n.allocationID)
}

func (n *NodeSandboxClient) Move(ctx context.Context, srcPath, dstPath string, options MoveOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if srcPath == "" || dstPath == "" {
		return errSrcDstPathRequired()
	}
	err := n.rpcClient().Move(ctx, srcPath, dstPath, options.Overwrite)
	return mapRPCError(err, "sandbox move", n.allocationID)
}

func (n *NodeSandboxClient) Chmod(ctx context.Context, path string, mode uint32, options ChmodOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if path == "" {
		return errPathRequired()
	}
	if mode > 0o7777 {
		return modeError("mode")
	}
	err := n.rpcClient().Chmod(ctx, path, mode, options.Recursive)
	return mapRPCError(err, "sandbox chmod", n.allocationID)
}

func (n *NodeSandboxClient) Touch(ctx context.Context, path string, options TouchOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if path == "" {
		return errPathRequired()
	}
	err := n.rpcClient().Touch(ctx, path, !options.NoCreate, options.MtimeNS)
	return mapRPCError(err, "sandbox touch", n.allocationID)
}
