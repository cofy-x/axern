package workspace

import (
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func NewFromConfig(cfg config.Config) *Manager {
	c := cfg.RuntimeConfig
	return New(Options{
		Root: c.WorkspaceRoot, MetadataRoot: filepath.Join(cfg.RootDir, "workspace-owners"),
		NodeID: cfg.ControlPlaneNodeID, Capacity: c.WorkspaceCapacityBytes, Inodes: c.WorkspaceInodeCapacity,
		ProjectBudget: c.WorkspaceProjectBudget,
		FilestoreRoot: c.FilestoreDir, FilestoreReserve: c.FilestoreSystemReserveBytes,
	})
}
