package oci

import (
	"fmt"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

// EnforceProcessSecurity is unconditional after resolving the execution user.
// Root setup retains its explicit capability ceiling; unprivileged processes
// cannot inherit root capability sets or acquire privilege through exec.
func EnforceProcessSecurity(process *spec.Process) {
	if process == nil {
		return
	}
	process.NoNewPrivileges = true
	if process.User.UID != 0 {
		process.User.AdditionalGids = nil
		process.Capabilities = &spec.LinuxCapabilities{
			Bounding: []string{}, Effective: []string{}, Permitted: []string{},
			Inheritable: []string{}, Ambient: []string{},
		}
	} else if process.Capabilities != nil {
		process.Capabilities.Ambient = nil
	}
}

// ValidateProcessSecurity verifies a persisted projection without rewriting it.
// Cleanup may still use legacy projections; only activation calls this fence.
func ValidateProcessSecurity(process *spec.Process) error {
	if process == nil || !process.NoNewPrivileges {
		return fmt.Errorf("managed process lacks no-new-privileges protection")
	}
	if process.Capabilities != nil && len(process.Capabilities.Ambient) != 0 {
		return fmt.Errorf("managed process has ambient capabilities")
	}
	if process.User.UID != 0 {
		if process.Capabilities == nil || len(process.User.AdditionalGids) != 0 {
			return fmt.Errorf("non-root process lacks explicit unprivileged credentials")
		}
		caps := process.Capabilities
		if len(caps.Bounding)+len(caps.Effective)+len(caps.Permitted)+len(caps.Inheritable)+len(caps.Ambient) != 0 {
			return fmt.Errorf("non-root process retains capabilities")
		}
	}
	return nil
}
