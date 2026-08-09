package placementkernel

import (
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
	CapabilityRequirements         []*capabilityv1.CapabilityKey
	NodeSelector                   map[string]string
}

// ResolveRequestForNode derives representation- and backend-specific
// requirements from the same locked NodeSummary used for eligibility. The
// returned request is independent from the base request and is safe to persist
// with an admission decision.
func ResolveRequestForNode(base *Request, summary *nodev1.NodeSummary, now time.Time) *Request {
	if base == nil {
		return nil
	}
	out := *base
	out.Ports = append([]string(nil), base.Ports...)
	out.CapabilityRequirements = cloneCapabilityKeys(base.CapabilityRequirements)
	out.NodeSelector = cloneLabels(base.NodeSelector)
	for _, locality := range summary.GetLocality() {
		if locality.GetKey() == base.GetRootfsKey() && locality.GetMountType() == nodev1.MountType_MOUNT_TYPE_EROFS {
			out.MountType = nodev1.MountType_MOUNT_TYPE_EROFS
			out.CapabilityRequirements = appendCapability(out.CapabilityRequirements, capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS))
			break
		}
	}
	if out.GetNetwork() != "host" {
		for _, platform := range []capabilityv1.PlatformCapability{
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
		} {
			key := capabilitycontract.PlatformKey(platform)
			if _, available := capabilitycontract.AvailableObservation(summary.GetCapabilitySnapshot(), key, now); available {
				out.CapabilityRequirements = appendCapability(out.CapabilityRequirements, key)
				break
			}
		}
	}
	return &out
}

func appendCapability(in []*capabilityv1.CapabilityKey, key *capabilityv1.CapabilityKey) []*capabilityv1.CapabilityKey {
	want, err := capabilitycontract.KeyID(key)
	if err != nil {
		return in
	}
	for _, existing := range in {
		id, _ := capabilitycontract.KeyID(existing)
		if id == want {
			return in
		}
	}
	return append(in, capabilitycontract.CloneKey(key))
}

func cloneCapabilityKeys(in []*capabilityv1.CapabilityKey) []*capabilityv1.CapabilityKey {
	out := make([]*capabilityv1.CapabilityKey, 0, len(in))
	for _, key := range in {
		if key != nil {
			out = append(out, proto.Clone(key).(*capabilityv1.CapabilityKey))
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
