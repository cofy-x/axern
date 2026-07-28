package bpfnet

import (
	"time"

	internaldataplane "github.com/cofy-x/axern/network/bpfnet/internal/dataplane"
)

type dataplaneAdapter struct {
	inner internaldataplane.Interface
}

func defaultDataplaneFactory(cfg Config, _ commandRunner) dataplane {
	return &dataplaneAdapter{
		inner: internaldataplane.New(toInternalConfig(cfg)),
	}
}

func (d *dataplaneAdapter) EnsureAttached(uplinks []string, ipRange string, nativeRoutingCIDRs []string, services []Service) (dataplaneAttachment, error) {
	attachment, err := d.inner.EnsureAttached(uplinks, ipRange, nativeRoutingCIDRs, toInternalServices(services))
	return dataplaneAttachment{
		LocalAddresses:       append([]string(nil), attachment.LocalAddresses...),
		LocalhostTCPDNAT:     attachment.LocalhostTCPDNAT,
		LocalhostAttachError: attachment.LocalhostAttachError,
	}, err
}

func (d *dataplaneAdapter) UpsertService(service Service) error {
	return d.inner.UpsertService(toInternalService(service))
}

func (d *dataplaneAdapter) DeleteService(service Service) error {
	return d.inner.DeleteService(toInternalService(service))
}

func (d *dataplaneAdapter) CleanupStaleSNATMappings(policy SNATGCPolicy) (SNATGCResult, error) {
	result, err := d.inner.CleanupStaleSNATMappings(toInternalSNATGCPolicy(policy))
	return SNATGCResult{
		FwdScanned: result.FwdScanned,
		FwdDeleted: result.FwdDeleted,
		RevScanned: result.RevScanned,
		RevDeleted: result.RevDeleted,
	}, err
}

func toInternalConfig(cfg Config) internaldataplane.Config {
	return internaldataplane.Config{
		PinPath:          cfg.PinPath,
		MapSize:          cfg.MapSize,
		SNATMapSize:      cfg.SNATMapSize,
		LocalOutCompat:   cfg.LocalOutCompat,
		IptablesFallback: cfg.IptablesFallback,
	}
}

func toInternalService(service Service) internaldataplane.Service {
	return internaldataplane.Service{
		Protocol:   service.Protocol,
		HostPort:   service.HostPort,
		TargetIP:   service.TargetIP,
		TargetPort: service.TargetPort,
	}
}

func toInternalServices(services []Service) []internaldataplane.Service {
	out := make([]internaldataplane.Service, 0, len(services))
	for _, service := range services {
		out = append(out, toInternalService(service))
	}
	return out
}

func toInternalSNATGCPolicy(policy SNATGCPolicy) internaldataplane.SNATGCPolicy {
	return internaldataplane.SNATGCPolicy{
		TCPIdleNanos:      durationNanos(policy.TCPIdleTimeout),
		TCPClosingNanos:   durationNanos(policy.TCPClosingTimeout),
		DatagramIdleNanos: durationNanos(policy.DatagramIdleTimeout),
	}
}

func durationNanos(value time.Duration) uint64 {
	nanos := value.Nanoseconds()
	if nanos <= 0 {
		return 0
	}
	return uint64(nanos)
}
