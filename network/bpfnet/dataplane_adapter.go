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

func (d *dataplaneAdapter) EnsureAttached(uplinks []string, ipRange string, nativeRoutingCIDRs []string) (dataplaneAttachment, error) {
	_, err := d.inner.EnsureAttached(uplinks, ipRange, nativeRoutingCIDRs)
	return dataplaneAttachment{}, err
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
		PinPath:     cfg.PinPath,
		SNATMapSize: cfg.SNATMapSize,
	}
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
