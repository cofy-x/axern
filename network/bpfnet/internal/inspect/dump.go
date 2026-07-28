package inspect

import (
	"fmt"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cofy-x/axern/network/bpfnet/internal/tcprog"
)

func DumpMap(pinPath, name string, limit int, raw bool) (Dump, error) {
	if !isKnownMap(name) {
		return Dump{}, fmt.Errorf("unknown map %q", name)
	}
	if limit <= 0 {
		limit = 100
	}
	if highChurnMaps[name] && !raw {
		return Dump{}, fmt.Errorf("%s is high-churn; use --raw to dump it", name)
	}

	path := filepath.Join(pinPath, name)
	m, err := ebpf.LoadPinnedMap(path, nil)
	if err != nil {
		return Dump{}, fmt.Errorf("open pinned map %s: %w", path, err)
	}
	defer m.Close()

	entries, truncated, err := dumpEntries(m, name, limit, raw)
	if err != nil {
		return Dump{}, err
	}
	return Dump{MapName: name, Raw: raw, Limit: limit, Entries: entries, Truncated: truncated}, nil
}

func dumpEntries(m *ebpf.Map, name string, limit int, raw bool) ([]Entry, bool, error) {
	if raw {
		return dumpRawEntries(m, name, limit)
	}

	switch name {
	case MapService:
		return dumpTyped(m, limit, formatServiceKey, formatServiceValue)
	case MapConfig:
		return dumpConfig(m, limit)
	case MapLocalAddr:
		return dumpTyped(m, limit, formatLocalAddrKey, func(value tcprog.DataplaneLocalAddrValue) any {
			return map[string]any{"present": value.Present != 0}
		})
	case MapUplinkAddr:
		return dumpTyped(m, limit, formatUplinkKey, formatUplinkValue)
	case MapNativeRoute:
		return dumpTyped(m, limit, formatNativeRouteKey, func(value tcprog.DataplaneNativeRouteValue) any {
			return map[string]any{"present": value.Present != 0}
		})
	case MapStats:
		return dumpStats(m, limit)
	default:
		return dumpRawEntries(m, name, limit)
	}
}

func dumpTyped[K any, V any](m *ebpf.Map, limit int, formatKey func(K) any, formatValue func(V) any) ([]Entry, bool, error) {
	var entries []Entry
	iterator := m.Iterate()
	var key K
	var value V
	for iterator.Next(&key, &value) {
		if limit > 0 && len(entries) >= limit {
			return entries, true, iterator.Err()
		}
		entries = append(entries, Entry{Key: formatKey(key), Value: formatValue(value)})
	}
	return entries, false, iterator.Err()
}

func dumpConfig(m *ebpf.Map, limit int) ([]Entry, bool, error) {
	if limit <= 0 {
		limit = 1
	}
	var entries []Entry
	for key := uint32(0); key < 1; key++ {
		var value tcprog.DataplaneConfigValue
		if err := m.Lookup(key, &value); err != nil {
			continue
		}
		entries = append(entries, Entry{
			Key: key,
			Value: map[string]any{
				"sandbox_addr":          ipv4FromUint32(value.SandboxAddr),
				"sandbox_mask":          ipv4FromUint32(value.SandboxMask),
				"native_routes_enabled": value.NativeRoutesEnabled != 0,
			},
		})
		if len(entries) >= limit {
			return entries, true, nil
		}
	}
	return entries, false, nil
}

func dumpStats(m *ebpf.Map, limit int) ([]Entry, bool, error) {
	if limit <= 0 || limit > len(statNames) {
		limit = len(statNames)
	}
	entries := make([]Entry, 0, limit)
	for index := uint32(0); int(index) < limit; index++ {
		var perCPU []uint64
		if err := m.Lookup(index, &perCPU); err != nil {
			continue
		}
		var total uint64
		for _, value := range perCPU {
			total += value
		}
		entries = append(entries, Entry{
			Key: statNames[index],
			Value: map[string]any{
				"index": index,
				"total": total,
			},
		})
	}
	return entries, limit < len(statNames), nil
}

func dumpRawEntries(m *ebpf.Map, name string, limit int) ([]Entry, bool, error) {
	switch name {
	case MapService:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneServiceKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneServiceValue) any { return rawStruct(value) },
		)
	case MapLocalAddr:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneLocalAddrKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneLocalAddrValue) any { return rawStruct(value) },
		)
	case MapRevNAT:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneRevNatKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneRevNatValue) any { return rawStruct(value) },
		)
	case MapConfig:
		return dumpTyped(m, limit,
			func(key uint32) any { return rawStruct(key) },
			func(value tcprog.DataplaneConfigValue) any { return rawStruct(value) },
		)
	case MapHostNetNS:
		return dumpTyped(m, limit,
			func(key uint32) any { return rawStruct(key) },
			func(value uint64) any { return rawStruct(value) },
		)
	case MapUplinkAddr:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneUplinkAddrKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneUplinkAddrValue) any { return rawStruct(value) },
		)
	case MapNativeRoute:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneNativeRouteKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneNativeRouteValue) any { return rawStruct(value) },
		)
	case MapSNATFwd:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneSnatFwdKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneSnatFwdValue) any { return rawStruct(value) },
		)
	case MapSNATRev:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneSnatRevKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneSnatRevValue) any { return rawStruct(value) },
		)
	case MapSNATRevMarker:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneSnatRevKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneSnatRevMarkerValue) any { return rawStruct(value) },
		)
	case MapLocalhostSock:
		return dumpTyped(m, limit,
			func(key tcprog.DataplaneLocalhostSockKey) any { return rawStruct(key) },
			func(value tcprog.DataplaneLocalhostSockValue) any { return rawStruct(value) },
		)
	case MapStats:
		return dumpStats(m, limit)
	default:
		return nil, false, fmt.Errorf("raw dump unsupported for map %s", name)
	}
}
