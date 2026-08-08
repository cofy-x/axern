package placement

import "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"

type Request struct {
	RootfsKey                      string
	RootfsType                     nodev1.RootfsType
	MountType                      nodev1.MountType
	Runtime                        string
	RequiresHostPort               bool
	RequestedCpuMilli              int64
	RequestedMemoryBytes           int64
	RequestedEphemeralStorageBytes int64
	Ports                          []string
	Network                        string
	CapabilityRequirements         []string
	NodeSelector                   map[string]string
}

func (r *Request) GetRootfsKey() string {
	if r == nil {
		return ""
	}
	return r.RootfsKey
}

func (r *Request) GetRootfsType() nodev1.RootfsType {
	if r == nil {
		return nodev1.RootfsType_ROOTFS_TYPE_UNSPECIFIED
	}
	return r.RootfsType
}

func (r *Request) GetMountType() nodev1.MountType {
	if r == nil {
		return nodev1.MountType_MOUNT_TYPE_UNSPECIFIED
	}
	return r.MountType
}

func (r *Request) GetRuntime() string {
	if r == nil {
		return ""
	}
	return r.Runtime
}

func (r *Request) GetRequiresHostPort() bool {
	return r != nil && r.RequiresHostPort
}

func (r *Request) GetRequestedCpuMilli() int64 {
	if r == nil {
		return 0
	}
	return r.RequestedCpuMilli
}

func (r *Request) GetRequestedMemoryBytes() int64 {
	if r == nil {
		return 0
	}
	return r.RequestedMemoryBytes
}

func (r *Request) GetRequestedEphemeralStorageBytes() int64 {
	if r == nil {
		return 0
	}
	return r.RequestedEphemeralStorageBytes
}

func (r *Request) GetPorts() []string {
	if r == nil {
		return nil
	}
	return r.Ports
}

func (r *Request) GetNetwork() string {
	if r == nil {
		return ""
	}
	return r.Network
}

func (r *Request) GetCapabilityRequirements() []string {
	if r == nil {
		return nil
	}
	return r.CapabilityRequirements
}

func (r *Request) GetNodeSelector() map[string]string {
	if r == nil {
		return nil
	}
	return r.NodeSelector
}
