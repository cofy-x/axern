package placementkernel

import (
	"fmt"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
)

// Request is the durable-admission-safe, fully derived placement contract.
// It contains platform requirements inferred from the workload rather than
// user-controlled platform capability names.
type Request struct {
	RootfsKey                       string
	RootfsType                      nodev1.RootfsType
	MountType                       nodev1.MountType
	Runtime                         string
	MemoryLimitBytes                int64
	RootfsWritable                  bool
	EphemeralStorageLimitBytes      int64
	RequiresHostPort                bool
	RequestedCpuMilli               int64
	RequestedMemoryBytes            int64
	RequestedEphemeralStorageBytes  int64
	Ports                           []string
	Network                         string
	NetworkBackend                  string
	CapabilityRequirements          []*capabilityv1.CapabilityKey
	ExtensionCapabilityRequirements []*capabilityv1.ExtensionCapabilityRequirement
	NodeSelector                    map[string]string
}

// ResolveRequestForNode derives representation- and backend-specific
// requirements from the same locked NodeSummary used for eligibility. The
// returned request is independent from the base request and is safe to persist
// with an admission decision.
func ResolveRequestForNode(base *Request, summary *nodev1.NodeSummary, now time.Time) (*Request, error) {
	if base == nil {
		return nil, nil
	}
	out := *base
	out.Ports = append([]string(nil), base.Ports...)
	out.ExtensionCapabilityRequirements = cloneExtensionRequirements(base.ExtensionCapabilityRequirements)
	out.NodeSelector = cloneLabels(base.NodeSelector)
	erofsBacking := false
	for _, locality := range summary.GetLocality() {
		if locality.GetKey() == base.GetRootfsKey() && locality.GetMountType() == nodev1.MountType_MOUNT_TYPE_EROFS {
			out.MountType = nodev1.MountType_MOUNT_TYPE_EROFS
			erofsBacking = true
			break
		}
	}
	var networkErr error
	if out.GetNetwork() != "host" {
		out.NetworkBackend = capabilitycontract.AvailableNetworkBackend(summary.GetCapabilitySnapshot(), now)
		if out.NetworkBackend == "" {
			networkErr = fmt.Errorf("node has no available network backend")
		}
	}
	input := capabilitycontract.RequirementInput{
		RuntimeName:                 out.Runtime,
		HasPorts:                    len(out.Ports) > 0 || out.RequiresHostPort,
		NetworkMode:                 out.Network,
		NetworkBackend:              out.NetworkBackend,
		MemoryLimitBytes:            out.MemoryLimitBytes,
		RootfsWritable:              out.RootfsWritable,
		EphemeralStorageLimitBytes:  out.EphemeralStorageLimitBytes,
		EROFSBacking:                erofsBacking,
		ExtensionCapabilityRequests: out.ExtensionCapabilityRequirements,
	}
	var requirements []*capabilityv1.CapabilityKey
	var err error
	if networkErr != nil {
		requirements, err = capabilitycontract.DeriveRequestStaticRequirements(input)
	} else {
		requirements, err = capabilitycontract.DeriveRequirements(input)
	}
	if err != nil {
		return &out, fmt.Errorf("derive node-specific capability requirements: %w", err)
	}
	out.CapabilityRequirements = requirements
	return &out, networkErr
}

func cloneExtensionRequirements(in []*capabilityv1.ExtensionCapabilityRequirement) []*capabilityv1.ExtensionCapabilityRequirement {
	out := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(in))
	for _, requirement := range in {
		if requirement != nil {
			out = append(out, proto.Clone(requirement).(*capabilityv1.ExtensionCapabilityRequirement))
		}
	}
	return out
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
func (r *Request) GetRequiresHostPort() bool { return r != nil && r.RequiresHostPort }
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
func (r *Request) GetCapabilityRequirements() []*capabilityv1.CapabilityKey {
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
