package natbench

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	procEphemeralPortRange = "/proc/sys/net/ipv4/ip_local_port_range"
	procTCPTimeWaitReuse   = "/proc/sys/net/ipv4/tcp_tw_reuse"
	procTCPFinTimeout      = "/proc/sys/net/ipv4/tcp_fin_timeout"
	procSockstat           = "/proc/net/sockstat"
	procTCPTable           = "/proc/net/tcp"
	procTCP6Table          = "/proc/net/tcp6"
)

type ClientResourceSnapshot struct {
	CollectedAt          time.Time     `json:"collectedAt,omitempty"`
	OpenFileLimit        uint64        `json:"openFileLimit,omitempty"`
	EphemeralPortFirst   uint64        `json:"ephemeralPortFirst,omitempty"`
	EphemeralPortLast    uint64        `json:"ephemeralPortLast,omitempty"`
	EphemeralPortCount   uint64        `json:"ephemeralPortCount,omitempty"`
	TCPTimeWaitReuse     uint64        `json:"tcpTimeWaitReuse,omitempty"`
	TCPFinTimeoutSeconds uint64        `json:"tcpFinTimeoutSeconds,omitempty"`
	TCPTimeWaitCount     uint64        `json:"tcpTimeWaitCount,omitempty"`
	TCPTimeWaitSource    string        `json:"tcpTimeWaitSource,omitempty"`
	Sockstat             SocketStats   `json:"sockstat,omitempty"`
	TCPTable             TCPTableStats `json:"tcpTable,omitempty"`
	CollectionErrors     []string      `json:"collectionErrors,omitempty"`
}

type ClientResourceDelta struct {
	SocketsUsed         int64 `json:"socketsUsed,omitempty"`
	TCPInUse            int64 `json:"tcpInUse,omitempty"`
	TCPOrphan           int64 `json:"tcpOrphan,omitempty"`
	TCPTimeWait         int64 `json:"tcpTimeWait,omitempty"`
	TCPAlloc            int64 `json:"tcpAlloc,omitempty"`
	TCPMem              int64 `json:"tcpMem,omitempty"`
	UDPInUse            int64 `json:"udpInUse,omitempty"`
	UDPMem              int64 `json:"udpMem,omitempty"`
	RAWInUse            int64 `json:"rawInUse,omitempty"`
	FRAGInUse           int64 `json:"fragInUse,omitempty"`
	FRAGMemory          int64 `json:"fragMemory,omitempty"`
	TCPTableEntries     int64 `json:"tcpTableEntries,omitempty"`
	TCPTableEstablished int64 `json:"tcpTableEstablished,omitempty"`
	TCPTableSynSent     int64 `json:"tcpTableSynSent,omitempty"`
	TCPTableSynRecv     int64 `json:"tcpTableSynRecv,omitempty"`
	TCPTableFinWait1    int64 `json:"tcpTableFinWait1,omitempty"`
	TCPTableFinWait2    int64 `json:"tcpTableFinWait2,omitempty"`
	TCPTableTimeWait    int64 `json:"tcpTableTimeWait,omitempty"`
	TCPTableCloseWait   int64 `json:"tcpTableCloseWait,omitempty"`
	TCPTableListen      int64 `json:"tcpTableListen,omitempty"`
}

type SocketStats struct {
	SocketsUsed uint64 `json:"socketsUsed,omitempty"`
	TCPInUse    uint64 `json:"tcpInUse,omitempty"`
	TCPOrphan   uint64 `json:"tcpOrphan,omitempty"`
	TCPTimeWait uint64 `json:"tcpTimeWait,omitempty"`
	TCPAlloc    uint64 `json:"tcpAlloc,omitempty"`
	TCPMem      uint64 `json:"tcpMem,omitempty"`
	UDPInUse    uint64 `json:"udpInUse,omitempty"`
	UDPMem      uint64 `json:"udpMem,omitempty"`
	RAWInUse    uint64 `json:"rawInUse,omitempty"`
	FRAGInUse   uint64 `json:"fragInUse,omitempty"`
	FRAGMemory  uint64 `json:"fragMemory,omitempty"`
}

type TCPTableStats struct {
	Entries     uint64 `json:"entries,omitempty"`
	Established uint64 `json:"established,omitempty"`
	SynSent     uint64 `json:"synSent,omitempty"`
	SynRecv     uint64 `json:"synRecv,omitempty"`
	FinWait1    uint64 `json:"finWait1,omitempty"`
	FinWait2    uint64 `json:"finWait2,omitempty"`
	TimeWait    uint64 `json:"timeWait,omitempty"`
	Close       uint64 `json:"close,omitempty"`
	CloseWait   uint64 `json:"closeWait,omitempty"`
	LastAck     uint64 `json:"lastAck,omitempty"`
	Listen      uint64 `json:"listen,omitempty"`
	Closing     uint64 `json:"closing,omitempty"`
}

func ReadClientResourceSnapshot() ClientResourceSnapshot {
	snapshot := ClientResourceSnapshot{
		CollectedAt: time.Now().UTC(),
	}

	var rlimit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &rlimit); err != nil {
		snapshot.addCollectionError("get rlimit nofile: %v", err)
	} else {
		snapshot.OpenFileLimit = rlimit.Cur
	}

	if data, err := os.ReadFile(procEphemeralPortRange); err != nil {
		snapshot.addCollectionError("read %s: %v", procEphemeralPortRange, err)
	} else if first, last, err := parseEphemeralPortRange(string(data)); err != nil {
		snapshot.addCollectionError("parse %s: %v", procEphemeralPortRange, err)
	} else {
		snapshot.EphemeralPortFirst = first
		snapshot.EphemeralPortLast = last
		snapshot.EphemeralPortCount = last - first + 1
	}

	if value, ok, err := readOptionalProcUint64(procTCPTimeWaitReuse); err != nil {
		snapshot.addCollectionError("read %s: %v", procTCPTimeWaitReuse, err)
	} else if ok {
		snapshot.TCPTimeWaitReuse = value
	}

	if value, ok, err := readOptionalProcUint64(procTCPFinTimeout); err != nil {
		snapshot.addCollectionError("read %s: %v", procTCPFinTimeout, err)
	} else if ok {
		snapshot.TCPFinTimeoutSeconds = value
	}

	sockstatCollected := false
	if data, err := os.ReadFile(procSockstat); err != nil {
		if !os.IsNotExist(err) {
			snapshot.addCollectionError("read %s: %v", procSockstat, err)
		}
	} else if stats, err := parseSockstat(string(data)); err != nil {
		snapshot.addCollectionError("parse %s: %v", procSockstat, err)
	} else {
		snapshot.Sockstat = stats
		sockstatCollected = true
	}

	if stats, collected, err := readTCPTableStats(); err != nil {
		snapshot.addCollectionError("read tcp table: %v", err)
	} else if collected {
		snapshot.TCPTable = stats
	}
	snapshot.setDerivedTCPTimeWait(sockstatCollected)

	return snapshot
}

func ClientResourcesDelta(before, after ClientResourceSnapshot) ClientResourceDelta {
	return ClientResourceDelta{
		SocketsUsed: int64(after.Sockstat.SocketsUsed) - int64(before.Sockstat.SocketsUsed),
		TCPInUse:    int64(after.Sockstat.TCPInUse) - int64(before.Sockstat.TCPInUse),
		TCPOrphan:   int64(after.Sockstat.TCPOrphan) - int64(before.Sockstat.TCPOrphan),
		TCPTimeWait: int64(tcpTimeWaitCount(after)) - int64(tcpTimeWaitCount(before)),
		TCPAlloc:    int64(after.Sockstat.TCPAlloc) - int64(before.Sockstat.TCPAlloc),
		TCPMem:      int64(after.Sockstat.TCPMem) - int64(before.Sockstat.TCPMem),
		UDPInUse:    int64(after.Sockstat.UDPInUse) - int64(before.Sockstat.UDPInUse),
		UDPMem:      int64(after.Sockstat.UDPMem) - int64(before.Sockstat.UDPMem),
		RAWInUse:    int64(after.Sockstat.RAWInUse) - int64(before.Sockstat.RAWInUse),
		FRAGInUse:   int64(after.Sockstat.FRAGInUse) - int64(before.Sockstat.FRAGInUse),
		FRAGMemory:  int64(after.Sockstat.FRAGMemory) - int64(before.Sockstat.FRAGMemory),

		TCPTableEntries:     int64(after.TCPTable.Entries) - int64(before.TCPTable.Entries),
		TCPTableEstablished: int64(after.TCPTable.Established) - int64(before.TCPTable.Established),
		TCPTableSynSent:     int64(after.TCPTable.SynSent) - int64(before.TCPTable.SynSent),
		TCPTableSynRecv:     int64(after.TCPTable.SynRecv) - int64(before.TCPTable.SynRecv),
		TCPTableFinWait1:    int64(after.TCPTable.FinWait1) - int64(before.TCPTable.FinWait1),
		TCPTableFinWait2:    int64(after.TCPTable.FinWait2) - int64(before.TCPTable.FinWait2),
		TCPTableTimeWait:    int64(after.TCPTable.TimeWait) - int64(before.TCPTable.TimeWait),
		TCPTableCloseWait:   int64(after.TCPTable.CloseWait) - int64(before.TCPTable.CloseWait),
		TCPTableListen:      int64(after.TCPTable.Listen) - int64(before.TCPTable.Listen),
	}
}

type ClientResourceSampler struct {
	stop chan struct{}
	done chan struct{}

	mu      sync.Mutex
	peak    ClientResourceSnapshot
	samples uint64
}

func StartClientResourceSampler(interval time.Duration) *ClientResourceSampler {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	sampler := &ClientResourceSampler{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	sampler.observe(ReadClientResourceSnapshot())
	go sampler.run(interval)
	return sampler
}

func (s *ClientResourceSampler) Stop(final ClientResourceSnapshot) (ClientResourceSnapshot, uint64) {
	close(s.stop)
	<-s.done
	s.observe(final)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak, s.samples
}

func (s *ClientResourceSampler) run(interval time.Duration) {
	defer close(s.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.observe(ReadClientResourceSnapshot())
		case <-s.stop:
			return
		}
	}
}

func (s *ClientResourceSampler) observe(snapshot ClientResourceSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples++
	s.peak = mergeClientResourcePeak(s.peak, snapshot)
}

func mergeClientResourcePeak(current, candidate ClientResourceSnapshot) ClientResourceSnapshot {
	peak := current
	if candidate.CollectedAt.After(peak.CollectedAt) {
		peak.CollectedAt = candidate.CollectedAt
	}
	peak.OpenFileLimit = maxUint64(peak.OpenFileLimit, candidate.OpenFileLimit)
	peak.EphemeralPortFirst = minNonZeroUint64(peak.EphemeralPortFirst, candidate.EphemeralPortFirst)
	peak.EphemeralPortLast = maxUint64(peak.EphemeralPortLast, candidate.EphemeralPortLast)
	peak.EphemeralPortCount = maxUint64(peak.EphemeralPortCount, candidate.EphemeralPortCount)
	peak.TCPTimeWaitReuse = maxUint64(peak.TCPTimeWaitReuse, candidate.TCPTimeWaitReuse)
	peak.TCPFinTimeoutSeconds = maxUint64(peak.TCPFinTimeoutSeconds, candidate.TCPFinTimeoutSeconds)
	if tcpTimeWaitCount(candidate) >= tcpTimeWaitCount(peak) {
		peak.TCPTimeWaitCount = tcpTimeWaitCount(candidate)
		peak.TCPTimeWaitSource = candidate.TCPTimeWaitSource
	}
	peak.Sockstat = mergeSocketStatsPeak(peak.Sockstat, candidate.Sockstat)
	peak.TCPTable = mergeTCPTableStatsPeak(peak.TCPTable, candidate.TCPTable)
	peak.CollectionErrors = appendUniqueStrings(peak.CollectionErrors, candidate.CollectionErrors...)
	return peak
}

func mergeSocketStatsPeak(current, candidate SocketStats) SocketStats {
	return SocketStats{
		SocketsUsed: maxUint64(current.SocketsUsed, candidate.SocketsUsed),
		TCPInUse:    maxUint64(current.TCPInUse, candidate.TCPInUse),
		TCPOrphan:   maxUint64(current.TCPOrphan, candidate.TCPOrphan),
		TCPTimeWait: maxUint64(current.TCPTimeWait, candidate.TCPTimeWait),
		TCPAlloc:    maxUint64(current.TCPAlloc, candidate.TCPAlloc),
		TCPMem:      maxUint64(current.TCPMem, candidate.TCPMem),
		UDPInUse:    maxUint64(current.UDPInUse, candidate.UDPInUse),
		UDPMem:      maxUint64(current.UDPMem, candidate.UDPMem),
		RAWInUse:    maxUint64(current.RAWInUse, candidate.RAWInUse),
		FRAGInUse:   maxUint64(current.FRAGInUse, candidate.FRAGInUse),
		FRAGMemory:  maxUint64(current.FRAGMemory, candidate.FRAGMemory),
	}
}

func mergeTCPTableStatsPeak(current, candidate TCPTableStats) TCPTableStats {
	return TCPTableStats{
		Entries:     maxUint64(current.Entries, candidate.Entries),
		Established: maxUint64(current.Established, candidate.Established),
		SynSent:     maxUint64(current.SynSent, candidate.SynSent),
		SynRecv:     maxUint64(current.SynRecv, candidate.SynRecv),
		FinWait1:    maxUint64(current.FinWait1, candidate.FinWait1),
		FinWait2:    maxUint64(current.FinWait2, candidate.FinWait2),
		TimeWait:    maxUint64(current.TimeWait, candidate.TimeWait),
		Close:       maxUint64(current.Close, candidate.Close),
		CloseWait:   maxUint64(current.CloseWait, candidate.CloseWait),
		LastAck:     maxUint64(current.LastAck, candidate.LastAck),
		Listen:      maxUint64(current.Listen, candidate.Listen),
		Closing:     maxUint64(current.Closing, candidate.Closing),
	}
}

func maxUint64(a, b uint64) uint64 {
	if b > a {
		return b
	}
	return a
}

func minNonZeroUint64(a, b uint64) uint64 {
	if a == 0 {
		return b
	}
	if b == 0 || a < b {
		return a
	}
	return b
}

func (s *ClientResourceSnapshot) addCollectionError(format string, args ...any) {
	s.CollectionErrors = append(s.CollectionErrors, fmt.Sprintf(format, args...))
}

func readProcUint64(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func readOptionalProcUint64(path string) (uint64, bool, error) {
	value, err := readProcUint64(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return value, true, nil
}

func parseEphemeralPortRange(text string) (uint64, uint64, error) {
	fields := strings.Fields(text)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("expected two fields, got %d", len(fields))
	}
	first, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse first port %q: %w", fields[0], err)
	}
	last, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse last port %q: %w", fields[1], err)
	}
	if first == 0 || last == 0 || last < first {
		return 0, 0, fmt.Errorf("invalid range %d-%d", first, last)
	}
	return first, last, nil
}

func readTCPTableStats() (TCPTableStats, bool, error) {
	var out TCPTableStats
	collected := false
	for _, path := range []string{procTCPTable, procTCP6Table} {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return TCPTableStats{}, false, fmt.Errorf("read %s: %w", path, err)
		}
		stats, err := parseTCPTable(string(data))
		if err != nil {
			return TCPTableStats{}, false, fmt.Errorf("parse %s: %w", path, err)
		}
		out = mergeTCPTableStats(out, stats)
		collected = true
	}
	return out, collected, nil
}

func parseTCPTable(text string) (TCPTableStats, error) {
	var stats TCPTableStats
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "sl" {
			continue
		}
		if len(fields) < 4 {
			return TCPTableStats{}, fmt.Errorf("malformed tcp table line %q", line)
		}
		state, err := strconv.ParseUint(fields[3], 16, 8)
		if err != nil {
			return TCPTableStats{}, fmt.Errorf("parse tcp state %q: %w", fields[3], err)
		}
		stats.Entries++
		assignTCPTableState(&stats, state)
	}
	return stats, nil
}

func mergeTCPTableStats(a, b TCPTableStats) TCPTableStats {
	return TCPTableStats{
		Entries:     a.Entries + b.Entries,
		Established: a.Established + b.Established,
		SynSent:     a.SynSent + b.SynSent,
		SynRecv:     a.SynRecv + b.SynRecv,
		FinWait1:    a.FinWait1 + b.FinWait1,
		FinWait2:    a.FinWait2 + b.FinWait2,
		TimeWait:    a.TimeWait + b.TimeWait,
		Close:       a.Close + b.Close,
		CloseWait:   a.CloseWait + b.CloseWait,
		LastAck:     a.LastAck + b.LastAck,
		Listen:      a.Listen + b.Listen,
		Closing:     a.Closing + b.Closing,
	}
}

func assignTCPTableState(stats *TCPTableStats, state uint64) {
	switch state {
	case 0x01:
		stats.Established++
	case 0x02:
		stats.SynSent++
	case 0x03:
		stats.SynRecv++
	case 0x04:
		stats.FinWait1++
	case 0x05:
		stats.FinWait2++
	case 0x06:
		stats.TimeWait++
	case 0x07:
		stats.Close++
	case 0x08:
		stats.CloseWait++
	case 0x09:
		stats.LastAck++
	case 0x0A:
		stats.Listen++
	case 0x0B:
		stats.Closing++
	}
}

func (s *ClientResourceSnapshot) setDerivedTCPTimeWait(sockstatCollected bool) {
	if sockstatCollected {
		s.TCPTimeWaitCount = s.Sockstat.TCPTimeWait
		s.TCPTimeWaitSource = "sockstat"
		return
	}
	if s.TCPTable.Entries > 0 || s.TCPTable.TimeWait > 0 {
		s.TCPTimeWaitCount = s.TCPTable.TimeWait
		s.TCPTimeWaitSource = "tcp_table"
	}
}

func tcpTimeWaitCount(snapshot ClientResourceSnapshot) uint64 {
	if snapshot.TCPTimeWaitSource != "" || snapshot.TCPTimeWaitCount > 0 {
		return snapshot.TCPTimeWaitCount
	}
	if snapshot.Sockstat.TCPTimeWait > 0 {
		return snapshot.Sockstat.TCPTimeWait
	}
	return snapshot.TCPTable.TimeWait
}

func parseSockstat(text string) (SocketStats, error) {
	var stats SocketStats
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		section := strings.TrimSuffix(fields[0], ":")
		for i := 1; i+1 < len(fields); i += 2 {
			key := fields[i]
			value, err := strconv.ParseUint(fields[i+1], 10, 64)
			if err != nil {
				return SocketStats{}, fmt.Errorf("parse %s %s value %q: %w", section, key, fields[i+1], err)
			}
			assignSockstatValue(&stats, section, key, value)
		}
	}
	return stats, nil
}

func assignSockstatValue(stats *SocketStats, section, key string, value uint64) {
	switch section {
	case "sockets":
		if key == "used" {
			stats.SocketsUsed = value
		}
	case "TCP":
		switch key {
		case "inuse":
			stats.TCPInUse = value
		case "orphan":
			stats.TCPOrphan = value
		case "tw":
			stats.TCPTimeWait = value
		case "alloc":
			stats.TCPAlloc = value
		case "mem":
			stats.TCPMem = value
		}
	case "UDP":
		switch key {
		case "inuse":
			stats.UDPInUse = value
		case "mem":
			stats.UDPMem = value
		}
	case "RAW":
		if key == "inuse" {
			stats.RAWInUse = value
		}
	case "FRAG":
		switch key {
		case "inuse":
			stats.FRAGInUse = value
		case "memory":
			stats.FRAGMemory = value
		}
	}
}
