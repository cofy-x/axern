//go:build linux

package dataplane

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cofy-x/axern/network/bpfnet/internal/tcprog"
	"golang.org/x/sys/unix"
)

const (
	snatEntryReverse     = 1
	snatEntryAlias       = 2
	snatFlowActive       = 0
	snatFlowOrigClosing  = 1
	snatFlowReplyClosing = 2
	snatFlowClosing      = 3
	maxSNATGCPasses      = 4
)

func (d *linuxDataplane) syncLocalAddresses(current map[tcprog.DataplaneLocalAddrKey]tcprog.DataplaneLocalAddrValue) error {
	if err := syncPinnedMap(d.objects.LocalAddrMap, current); err != nil {
		return fmt.Errorf("sync local address map: %w", err)
	}
	return nil
}

func (d *linuxDataplane) syncUplinkAddresses(current map[tcprog.DataplaneUplinkAddrKey]tcprog.DataplaneUplinkAddrValue) error {
	if err := syncPinnedMap(d.objects.UplinkAddrMap, current); err != nil {
		return fmt.Errorf("sync uplink address map: %w", err)
	}
	return nil
}

func (d *linuxDataplane) syncConfig(ipRange string, nativeRouteCount int) error {
	_, network, err := net.ParseCIDR(ipRange)
	if err != nil {
		return fmt.Errorf("parse sandbox ip range %q: %w", ipRange, err)
	}
	ipv4 := network.IP.To4()
	if ipv4 == nil {
		return fmt.Errorf("sandbox ip range %q is not ipv4", ipRange)
	}
	mask := net.IP(network.Mask).To4()
	if mask == nil {
		return fmt.Errorf("sandbox ip range %q has non-ipv4 mask", ipRange)
	}

	key := uint32(0)
	value := tcprog.DataplaneConfigValue{
		SandboxAddr:         binary.BigEndian.Uint32(ipv4),
		SandboxMask:         binary.BigEndian.Uint32(mask),
		NativeRoutesEnabled: boolToUint32(nativeRouteCount > 0),
	}
	if err := d.objects.ConfigMap.Update(key, value, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("sync config map: %w", err)
	}
	return nil
}

func (d *linuxDataplane) syncHostNetnsCookie() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("open host netns probe socket: %w", err)
	}
	defer unix.Close(fd)

	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if err != nil {
		return fmt.Errorf("read host netns cookie: %w", err)
	}

	key := uint32(0)
	if err := d.objects.HostNetnsCookieMap.Update(key, cookie, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("sync host netns cookie map: %w", err)
	}
	return nil
}

func (d *linuxDataplane) syncNativeRoutes(cidrs []string) error {
	current := make(map[tcprog.DataplaneNativeRouteKey]tcprog.DataplaneNativeRouteValue)
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			return fmt.Errorf("parse native routing cidr %q: %w", cidr, err)
		}
		ipv4 := network.IP.To4()
		if ipv4 == nil {
			return fmt.Errorf("native routing cidr %q is not ipv4", cidr)
		}
		prefixLen, _ := network.Mask.Size()
		current[tcprog.DataplaneNativeRouteKey{
			Prefixlen: uint32(prefixLen),
			Addr:      binary.BigEndian.Uint32(ipv4),
		}] = tcprog.DataplaneNativeRouteValue{Present: 1}
	}
	if err := syncPinnedMap(d.objects.NativeRouteMap, current); err != nil {
		return fmt.Errorf("sync native route map: %w", err)
	}
	return nil
}

func (d *linuxDataplane) syncServices(services []Service) error {
	current := make(map[tcprog.DataplaneServiceKey]tcprog.DataplaneServiceValue)
	for _, service := range services {
		proto, ok := serviceProtocolNumber(service.Protocol)
		if !ok {
			continue
		}
		current[tcprog.DataplaneServiceKey{
			Proto:    proto,
			HostPort: service.HostPort,
		}] = tcprog.DataplaneServiceValue{
			TargetIp:   ipv4ToUint32(service.TargetIP),
			TargetPort: service.TargetPort,
		}
	}
	if err := syncPinnedMap(d.objects.ServiceMap, current); err != nil {
		return fmt.Errorf("sync service map: %w", err)
	}
	return nil
}

func (d *linuxDataplane) clearRevNatState() error {
	return clearPinnedMap[tcprog.DataplaneRevNatKey, tcprog.DataplaneRevNatValue](d.objects.RevNatMap)
}

func (d *linuxDataplane) clearSNATState() error {
	if err := clearPinnedMap[tcprog.DataplaneSnatFwdKey, tcprog.DataplaneSnatFwdValue](d.objects.SnatFwdMap); err != nil {
		return err
	}
	if err := clearPinnedMap[tcprog.DataplaneSnatRevKey, tcprog.DataplaneSnatRevValue](d.objects.SnatRevMap); err != nil {
		return err
	}
	return clearPinnedMap[tcprog.DataplaneSnatRevKey, tcprog.DataplaneSnatRevMarkerValue](d.objects.SnatRevMarkerMap)
}

func (d *linuxDataplane) CleanupStaleSNATMappings(policy SNATGCPolicy) (SNATGCResult, error) {
	if err := d.ensureLoaded(); err != nil {
		return SNATGCResult{}, err
	}
	now, err := monotonicNowNanos()
	if err != nil {
		return SNATGCResult{}, err
	}

	return cleanupStaleSNATMappings(d.objects.SnatFwdMap, d.objects.SnatRevMap, d.objects.SnatRevMarkerMap, now, policy)
}

func cleanupStaleSNATMappings(fwdMap, revMap, markerMap *ebpf.Map, now uint64, policy SNATGCPolicy) (SNATGCResult, error) {
	var total SNATGCResult
	for pass := 0; pass < maxSNATGCPasses; pass++ {
		result, err := cleanupStaleSNATFwdMappings(fwdMap, revMap, now, policy)
		if err != nil {
			return total, err
		}
		revResult, err := cleanupStaleSNATRevMappings(revMap, now, policy)
		if err != nil {
			return total, err
		}
		result.RevScanned += revResult.RevScanned
		result.RevDeleted += revResult.RevDeleted
		total = mergeSNATGCResult(total, result)
		if result.FwdDeleted == 0 && result.RevDeleted == 0 {
			break
		}
	}
	if err := cleanupStaleSNATRevMarkers(markerMap, now, policy); err != nil {
		return total, err
	}
	return total, nil
}

func mergeSNATGCResult(a, b SNATGCResult) SNATGCResult {
	return SNATGCResult{
		FwdScanned: a.FwdScanned + b.FwdScanned,
		FwdDeleted: a.FwdDeleted + b.FwdDeleted,
		RevScanned: a.RevScanned + b.RevScanned,
		RevDeleted: a.RevDeleted + b.RevDeleted,
	}
}

func cleanupStaleSNATFwdMappings(fwdMap, revMap *ebpf.Map, now uint64, policy SNATGCPolicy) (SNATGCResult, error) {
	var result SNATGCResult
	iterator := fwdMap.Iterate()
	var key tcprog.DataplaneSnatFwdKey
	var value tcprog.DataplaneSnatFwdValue
	var candidates []tcprog.DataplaneSnatFwdKey
	for iterator.Next(&key, &value) {
		result.FwdScanned++
		if !snatEntryExpired(now, value.LastSeenNs, snatIdleNanos(key.Proto, value.State, policy)) {
			continue
		}
		candidates = append(candidates, key)
	}
	if err := iterator.Err(); err != nil {
		return result, err
	}
	for _, key := range candidates {
		fwdDeleted, revDeleted, err := deleteSNATFwdMappingIfExpired(fwdMap, revMap, key, now, policy)
		if err != nil {
			return result, err
		}
		if fwdDeleted {
			result.FwdDeleted++
		}
		if revDeleted {
			result.RevDeleted++
		}
	}
	return result, nil
}

func snatReverseKeyForForward(key tcprog.DataplaneSnatFwdKey, value tcprog.DataplaneSnatFwdValue) tcprog.DataplaneSnatRevKey {
	return tcprog.DataplaneSnatRevKey{
		SrcIp:   key.DstIp,
		DstIp:   value.HostIp,
		SrcPort: key.DstPort,
		DstPort: value.TranslatedSrc,
		Proto:   key.Proto,
		Flags:   snatEntryReverse,
	}
}

func snatReverseKeyForAlias(key tcprog.DataplaneSnatRevKey, value tcprog.DataplaneSnatRevValue) tcprog.DataplaneSnatRevKey {
	return tcprog.DataplaneSnatRevKey{
		SrcIp:   key.DstIp,
		DstIp:   value.HostIp,
		SrcPort: key.DstPort,
		DstPort: value.TranslatedSrc,
		Proto:   key.Proto,
		Flags:   snatEntryReverse,
	}
}

func cleanupStaleSNATRevMappings(m *ebpf.Map, now uint64, policy SNATGCPolicy) (SNATGCResult, error) {
	var result SNATGCResult
	iterator := m.Iterate()
	var key tcprog.DataplaneSnatRevKey
	var value tcprog.DataplaneSnatRevValue
	var candidates []snatRevDeleteCandidate
	for iterator.Next(&key, &value) {
		result.RevScanned++
		if key.Flags == snatEntryAlias {
			expired, err := snatAliasMappingExpired(m, key, value, now, policy)
			if err != nil {
				return result, err
			}
			if !expired {
				continue
			}
			candidates = append(candidates, snatRevDeleteCandidate{key: key, alias: true})
			continue
		}
		if !snatEntryExpired(now, value.LastSeenNs, snatIdleNanos(key.Proto, value.State, policy)) {
			continue
		}
		candidates = append(candidates, snatRevDeleteCandidate{key: key})
	}
	if err := iterator.Err(); err != nil {
		return result, err
	}
	for _, candidate := range candidates {
		deleted, err := deleteSNATRevCandidateIfExpired(m, candidate, now, policy)
		if err != nil {
			return result, err
		}
		if !deleted {
			continue
		}
		result.RevDeleted++
	}
	return result, nil
}

type snatRevDeleteCandidate struct {
	key   tcprog.DataplaneSnatRevKey
	alias bool
}

func snatAliasMappingExpired(m *ebpf.Map, key tcprog.DataplaneSnatRevKey, value tcprog.DataplaneSnatRevValue, now uint64, policy SNATGCPolicy) (bool, error) {
	if value.TranslatedSrc == 0 {
		return true, nil
	}
	revKey := snatReverseKeyForAlias(key, value)
	var revValue tcprog.DataplaneSnatRevValue
	if err := m.Lookup(revKey, &revValue); err != nil {
		if isMapKeyNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if !snatAliasReverseValueMatches(value, revValue) {
		return true, nil
	}
	return snatEntryExpired(now, revValue.LastSeenNs, snatIdleNanos(revKey.Proto, revValue.State, policy)), nil
}

func snatAliasReverseValueMatches(aliasValue, revValue tcprog.DataplaneSnatRevValue) bool {
	return revValue.TargetIp == aliasValue.TargetIp &&
		revValue.TargetPort == aliasValue.TargetPort &&
		revValue.HostIp == aliasValue.HostIp
}

func deleteSNATFwdMappingIfExpired(fwdMap, revMap *ebpf.Map, key tcprog.DataplaneSnatFwdKey, now uint64, policy SNATGCPolicy) (bool, bool, error) {
	var current tcprog.DataplaneSnatFwdValue
	if err := fwdMap.Lookup(key, &current); err != nil {
		if isMapKeyNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}
	if !snatEntryExpired(now, current.LastSeenNs, snatIdleNanos(key.Proto, current.State, policy)) {
		return false, false, nil
	}
	revKey := snatReverseKeyForForward(key, current)
	revExpired, err := snatRevMappingExpired(revMap, revKey, now, policy)
	if err != nil || !revExpired {
		return false, false, err
	}
	if err := fwdMap.Delete(key); err != nil {
		if isMapKeyNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}
	revDeleted, err := deleteSNATRevMappingIfExpired(revMap, revKey, now, policy)
	return true, revDeleted, err
}

func snatRevMappingExpired(m *ebpf.Map, key tcprog.DataplaneSnatRevKey, now uint64, policy SNATGCPolicy) (bool, error) {
	var current tcprog.DataplaneSnatRevValue
	if err := m.Lookup(key, &current); err != nil {
		if isMapKeyNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return snatEntryExpired(now, current.LastSeenNs, snatIdleNanos(key.Proto, current.State, policy)), nil
}

func deleteSNATRevMappingIfExpired(m *ebpf.Map, key tcprog.DataplaneSnatRevKey, now uint64, policy SNATGCPolicy) (bool, error) {
	expired, err := snatRevMappingExpired(m, key, now, policy)
	if err != nil || !expired {
		return false, err
	}
	if err := m.Delete(key); err != nil {
		if isMapKeyNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func deleteSNATAliasMappingIfExpired(m *ebpf.Map, key tcprog.DataplaneSnatRevKey, now uint64, policy SNATGCPolicy) (bool, error) {
	var value tcprog.DataplaneSnatRevValue
	if err := m.Lookup(key, &value); err != nil {
		if isMapKeyNotExist(err) {
			return false, nil
		}
		return false, err
	}
	expired, err := snatAliasMappingExpired(m, key, value, now, policy)
	if err != nil || !expired {
		return false, err
	}
	if err := m.Delete(key); err != nil {
		if isMapKeyNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func deleteSNATRevCandidateIfExpired(m *ebpf.Map, candidate snatRevDeleteCandidate, now uint64, policy SNATGCPolicy) (bool, error) {
	if candidate.alias {
		return deleteSNATAliasMappingIfExpired(m, candidate.key, now, policy)
	}
	return deleteSNATRevMappingIfExpired(m, candidate.key, now, policy)
}

func cleanupStaleSNATRevMarkers(m *ebpf.Map, now uint64, policy SNATGCPolicy) error {
	iterator := m.Iterate()
	var key tcprog.DataplaneSnatRevKey
	var value tcprog.DataplaneSnatRevMarkerValue
	for iterator.Next(&key, &value) {
		if !snatEntryExpired(now, value.LastSeenNs, snatIdleNanos(key.Proto, snatFlowActive, policy)) {
			continue
		}
		if err := m.Delete(key); err != nil && !isMapKeyNotExist(err) {
			return err
		}
	}
	return iterator.Err()
}

func snatEntryExpired(now, lastSeen, idleNanos uint64) bool {
	if idleNanos == 0 {
		return false
	}
	if lastSeen == 0 {
		return true
	}
	if now <= lastSeen {
		return false
	}
	return now-lastSeen >= idleNanos
}

func snatIdleNanos(proto, state uint8, policy SNATGCPolicy) uint64 {
	switch proto {
	case unix.IPPROTO_TCP:
		if state == snatFlowClosing && policy.TCPClosingNanos > 0 {
			return policy.TCPClosingNanos
		}
		return policy.TCPIdleNanos
	case unix.IPPROTO_UDP, unix.IPPROTO_ICMP:
		return policy.DatagramIdleNanos
	default:
		return policy.DatagramIdleNanos
	}
}

func snatFlowIsClosing(state uint8) bool {
	return state != snatFlowActive
}

func monotonicNowNanos() (uint64, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, err
	}
	return uint64(ts.Sec)*1_000_000_000 + uint64(ts.Nsec), nil
}

func (d *linuxDataplane) clearLocalhostState() error {
	return clearPinnedMap[tcprog.DataplaneLocalhostSockKey, tcprog.DataplaneLocalhostSockValue](d.objects.LocalhostSockMap)
}

func (d *linuxDataplane) bumpKernelStat(index uint32) error {
	if !d.loaded {
		return nil
	}

	var current []uint64
	if err := d.objects.StatsMap.Lookup(index, &current); err != nil {
		return err
	}
	if len(current) == 0 {
		current = []uint64{1}
	} else {
		current[0]++
	}
	return d.objects.StatsMap.Update(index, current, ebpf.UpdateAny)
}

func serviceProtocolNumber(protocol string) (uint8, bool) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "tcp":
		return unix.IPPROTO_TCP, true
	case "udp":
		return unix.IPPROTO_UDP, true
	default:
		return 0, false
	}
}

func syncPinnedMap[K comparable, V comparable](m *ebpf.Map, current map[K]V) error {
	seen := make(map[K]struct{})
	iterator := m.Iterate()
	var key K
	var value V
	for iterator.Next(&key, &value) {
		next, ok := current[key]
		if !ok {
			if err := m.Delete(key); err != nil && !isMapKeyNotExist(err) {
				return err
			}
			continue
		}
		if value != next {
			if err := m.Update(key, next, ebpf.UpdateAny); err != nil {
				return err
			}
		}
		seen[key] = struct{}{}
	}
	if err := iterator.Err(); err != nil {
		return err
	}

	for key, value := range current {
		if _, ok := seen[key]; ok {
			continue
		}
		if err := m.Update(key, value, ebpf.UpdateAny); err != nil {
			return err
		}
	}
	return nil
}

func clearPinnedMap[K comparable, V any](m *ebpf.Map) error {
	iterator := m.Iterate()
	var key K
	var value V
	for iterator.Next(&key, &value) {
		if err := m.Delete(key); err != nil && !isMapKeyNotExist(err) {
			return err
		}
	}
	return iterator.Err()
}

func ipv4ToUint32(ip string) uint32 {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return 0
	}
	return binary.BigEndian.Uint32(parsed)
}

func boolToUint32(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

func isMapKeyNotExist(err error) bool {
	return err == nil || errors.Is(err, ebpf.ErrKeyNotExist)
}
