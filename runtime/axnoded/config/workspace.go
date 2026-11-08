//go:build !windows

package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c RuntimeConfig) ValidateWorkspaceStorage() error {
	if c.WorkspaceRoot == "" && c.WorkspaceCapacityBytes == 0 && c.WorkspaceInodeCapacity == 0 {
		return nil
	}
	if !filepath.IsAbs(c.WorkspaceRoot) || filepath.Clean(c.WorkspaceRoot) != c.WorkspaceRoot || strings.TrimSpace(c.WorkspaceRoot) != c.WorkspaceRoot || c.WorkspaceRoot == "/" || c.WorkspaceCapacityBytes <= 0 || c.WorkspaceCapacityBytes%512 != 0 || c.WorkspaceInodeCapacity <= 0 {
		return fmt.Errorf("workspace_root, aligned positive workspace_capacity_bytes and positive workspace_inode_capacity are required together")
	}
	mode, err := c.CgroupEnforcementMode()
	if err != nil || mode != CgroupEnforcementRequired {
		return fmt.Errorf("persistent Workspace storage requires cgroup_enforcement=required")
	}
	return nil
}
