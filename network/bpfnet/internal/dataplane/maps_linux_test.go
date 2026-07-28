//go:build linux

package dataplane

import (
	"testing"

	"github.com/cofy-x/axern/network/bpfnet/internal/tcprog"
)

func TestSNATReverseKeyForForward(t *testing.T) {
	key := tcprog.DataplaneSnatFwdKey{
		SrcIp:   0xac110002,
		DstIp:   0xc6336402,
		SrcPort: 49152,
		DstPort: 19080,
		Proto:   6,
	}
	value := tcprog.DataplaneSnatFwdValue{
		HostIp:        0x0a000005,
		TranslatedSrc: 40001,
	}

	revKey := snatReverseKeyForForward(key, value)

	if revKey.SrcIp != key.DstIp ||
		revKey.DstIp != value.HostIp ||
		revKey.SrcPort != key.DstPort ||
		revKey.DstPort != value.TranslatedSrc ||
		revKey.Proto != key.Proto ||
		revKey.Flags != snatEntryReverse {
		t.Fatalf("unexpected reverse key: %#v", revKey)
	}
}

func TestSNATReverseKeyForAlias(t *testing.T) {
	key := tcprog.DataplaneSnatRevKey{
		SrcIp:   0xac110002,
		DstIp:   0xc6336402,
		SrcPort: 49152,
		DstPort: 19080,
		Proto:   17,
		Flags:   snatEntryAlias,
	}
	value := tcprog.DataplaneSnatRevValue{
		HostIp:        0x0a000005,
		TranslatedSrc: 40001,
	}

	revKey := snatReverseKeyForAlias(key, value)

	if revKey.SrcIp != key.DstIp ||
		revKey.DstIp != value.HostIp ||
		revKey.SrcPort != key.DstPort ||
		revKey.DstPort != value.TranslatedSrc ||
		revKey.Proto != key.Proto ||
		revKey.Flags != snatEntryReverse {
		t.Fatalf("unexpected alias reverse key: %#v", revKey)
	}
}

func TestSNATAliasReverseValueMatches(t *testing.T) {
	aliasValue := tcprog.DataplaneSnatRevValue{
		TargetIp:   0xac110002,
		HostIp:     0x0a000005,
		TargetPort: 49152,
	}
	revValue := tcprog.DataplaneSnatRevValue{
		TargetIp:   aliasValue.TargetIp,
		HostIp:     aliasValue.HostIp,
		TargetPort: aliasValue.TargetPort,
	}

	if !snatAliasReverseValueMatches(aliasValue, revValue) {
		t.Fatalf("expected matching alias/reverse values")
	}

	revValue.TargetIp = 0xac110003
	if snatAliasReverseValueMatches(aliasValue, revValue) {
		t.Fatalf("expected target mismatch to break alias/reverse match")
	}
}

func TestSNATIdleNanosUsesClosingTimeoutOnlyAfterBothTCPDirectionsClose(t *testing.T) {
	policy := SNATGCPolicy{
		TCPIdleNanos:    5,
		TCPClosingNanos: 1,
	}

	if got := snatIdleNanos(6, 1, policy); got != policy.TCPIdleNanos {
		t.Fatalf("orig half-close idle nanos = %d, want %d", got, policy.TCPIdleNanos)
	}
	if got := snatIdleNanos(6, 2, policy); got != policy.TCPIdleNanos {
		t.Fatalf("reply half-close idle nanos = %d, want %d", got, policy.TCPIdleNanos)
	}
	if got := snatIdleNanos(6, snatFlowClosing, policy); got != policy.TCPClosingNanos {
		t.Fatalf("full-close idle nanos = %d, want %d", got, policy.TCPClosingNanos)
	}
}
