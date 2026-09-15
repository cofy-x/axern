//go:build linux

package dataplane

import (
	"fmt"

	"github.com/cilium/ebpf/rlimit"
	"github.com/cofy-x/axern/network/bpfnet/internal/tcprog"
	"github.com/vishvananda/netlink"
)

const (
	statsMapName         = "stats_map"
	configMapName        = "config_map"
	uplinkAddrMapName    = "uplink_addr_map"
	nativeRouteMapName   = "native_route_map"
	snatFwdMapName       = "snat_fwd_map"
	snatRevMapName       = "snat_rev_map"
	snatRevMarkerMapName = "snat_rev_marker_map"
)

type linuxDataplane struct {
	cfg     Config
	objects tcprog.DataplaneObjects
	loaded  bool
}

func New(cfg Config) Interface {
	return &linuxDataplane{cfg: cfg}
}

func (d *linuxDataplane) EnsureAttached(uplinks []string, ipRange string, nativeRoutingCIDRs []string) (Attachment, error) {
	if err := d.ensureLoaded(); err != nil {
		return Attachment{}, err
	}
	if ipRange == "" {
		_ = d.bumpKernelStat(KernelStatAttachError)
		return Attachment{}, markReconcileError(fmt.Errorf("sandbox ip range is required for egress snat"))
	}

	interfaceData, err := collectInterfaceData(uplinks)
	if err != nil {
		_ = d.bumpKernelStat(KernelStatAttachError)
		return Attachment{}, markTCProbeError(err)
	}
	if err := d.reconcileStateMaps(interfaceData, ipRange, nativeRoutingCIDRs); err != nil {
		_ = d.bumpKernelStat(KernelStatAttachError)
		return Attachment{}, markReconcileError(err)
	}
	if err := d.reconcileTCPrograms(uplinks); err != nil {
		_ = d.bumpKernelStat(KernelStatAttachError)
		return Attachment{}, markTCProbeError(err)
	}

	if err := d.pinPrograms(); err != nil {
		return Attachment{}, markReconcileError(err)
	}
	_ = d.bumpKernelStat(KernelStatAttachSuccess)
	return Attachment{}, nil
}

func (d *linuxDataplane) ensureLoaded() error {
	if err := rlimit.RemoveMemlock(); err != nil {
		return markReconcileError(fmt.Errorf("remove memlock rlimit: %w", err))
	}
	if !d.loaded {
		if err := d.loadObjects(); err != nil {
			return markReconcileError(err)
		}
	}
	return nil
}

func (d *linuxDataplane) reconcileStateMaps(interfaceData interfaceData, ipRange string, nativeRoutingCIDRs []string) error {
	nativeRouteCount := len(nativeRoutingCIDRs)
	reconcileSteps := []func() error{
		func() error { return d.syncUplinkAddresses(interfaceData.uplinkAddrs) },
		func() error { return d.syncConfig(ipRange, nativeRouteCount) },
		func() error { return d.syncNativeRoutes(nativeRoutingCIDRs) },
		d.clearSNATState,
	}
	for _, step := range reconcileSteps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (d *linuxDataplane) reconcileTCPrograms(uplinks []string) error {
	for _, uplink := range uplinks {
		if err := attachTCProgram(uplink, d.objects.DataplaneIngress, netlink.HANDLE_MIN_INGRESS, netlink.MakeHandle(0, 1), "axern-bpfnet-ingress"); err != nil {
			return err
		}
		if err := attachTCProgram(uplink, d.objects.DataplaneEgress, netlink.HANDLE_MIN_EGRESS, netlink.MakeHandle(0, 2), "axern-bpfnet-egress"); err != nil {
			return err
		}
	}
	return nil
}
