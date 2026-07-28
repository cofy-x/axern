package natbench

import "time"

func aggregateClientResourceSnapshots(samples []ClientResourceSnapshot) ClientResourceSnapshot {
	if len(samples) == 0 {
		return ClientResourceSnapshot{}
	}

	var errors []string
	for _, sample := range samples {
		errors = append(errors, sample.CollectionErrors...)
	}

	return ClientResourceSnapshot{
		CollectedAt:          latestClientResourceCollectedAt(samples),
		OpenFileLimit:        medianSnapshotUint64(samples, func(sample ClientResourceSnapshot) uint64 { return sample.OpenFileLimit }),
		EphemeralPortFirst:   medianSnapshotUint64(samples, func(sample ClientResourceSnapshot) uint64 { return sample.EphemeralPortFirst }),
		EphemeralPortLast:    medianSnapshotUint64(samples, func(sample ClientResourceSnapshot) uint64 { return sample.EphemeralPortLast }),
		EphemeralPortCount:   medianSnapshotUint64(samples, func(sample ClientResourceSnapshot) uint64 { return sample.EphemeralPortCount }),
		TCPTimeWaitReuse:     medianSnapshotUint64(samples, func(sample ClientResourceSnapshot) uint64 { return sample.TCPTimeWaitReuse }),
		TCPFinTimeoutSeconds: medianSnapshotUint64(samples, func(sample ClientResourceSnapshot) uint64 { return sample.TCPFinTimeoutSeconds }),
		TCPTimeWaitCount:     medianSnapshotUint64(samples, func(sample ClientResourceSnapshot) uint64 { return sample.TCPTimeWaitCount }),
		TCPTimeWaitSource:    mostCommonString(snapshotTCPTimeWaitSources(samples)),
		Sockstat:             aggregateSocketStats(snapshotSockstats(samples)),
		TCPTable:             aggregateTCPTableStats(snapshotTCPTables(samples)),
		CollectionErrors:     appendUniqueStrings(nil, errors...),
	}
}

func aggregateClientResourceDeltas(samples []ClientResourceDelta) ClientResourceDelta {
	if len(samples) == 0 {
		return ClientResourceDelta{}
	}
	return ClientResourceDelta{
		SocketsUsed: medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.SocketsUsed }),
		TCPInUse:    medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPInUse }),
		TCPOrphan:   medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPOrphan }),
		TCPTimeWait: medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTimeWait }),
		TCPAlloc:    medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPAlloc }),
		TCPMem:      medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPMem }),
		UDPInUse:    medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.UDPInUse }),
		UDPMem:      medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.UDPMem }),
		RAWInUse:    medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.RAWInUse }),
		FRAGInUse:   medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.FRAGInUse }),
		FRAGMemory:  medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.FRAGMemory }),

		TCPTableEntries:     medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableEntries }),
		TCPTableEstablished: medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableEstablished }),
		TCPTableSynSent:     medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableSynSent }),
		TCPTableSynRecv:     medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableSynRecv }),
		TCPTableFinWait1:    medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableFinWait1 }),
		TCPTableFinWait2:    medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableFinWait2 }),
		TCPTableTimeWait:    medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableTimeWait }),
		TCPTableCloseWait:   medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableCloseWait }),
		TCPTableListen:      medianDeltaInt64(samples, func(sample ClientResourceDelta) int64 { return sample.TCPTableListen }),
	}
}

func CombineClientResourceSnapshots(samples []ClientResourceSnapshot) ClientResourceSnapshot {
	if len(samples) == 0 {
		return ClientResourceSnapshot{}
	}
	var out ClientResourceSnapshot
	var errors []string
	for _, sample := range samples {
		if sample.CollectedAt.After(out.CollectedAt) {
			out.CollectedAt = sample.CollectedAt
		}
		out.OpenFileLimit += sample.OpenFileLimit
		out.EphemeralPortFirst = minNonZeroUint64(out.EphemeralPortFirst, sample.EphemeralPortFirst)
		out.EphemeralPortLast = maxUint64(out.EphemeralPortLast, sample.EphemeralPortLast)
		out.EphemeralPortCount += sample.EphemeralPortCount
		out.TCPTimeWaitReuse = maxUint64(out.TCPTimeWaitReuse, sample.TCPTimeWaitReuse)
		out.TCPFinTimeoutSeconds = maxUint64(out.TCPFinTimeoutSeconds, sample.TCPFinTimeoutSeconds)
		out.TCPTimeWaitCount += tcpTimeWaitCount(sample)
		out.Sockstat = addSocketStats(out.Sockstat, sample.Sockstat)
		out.TCPTable = addTCPTableStats(out.TCPTable, sample.TCPTable)
		errors = append(errors, sample.CollectionErrors...)
	}
	if out.TCPTimeWaitCount > 0 {
		out.TCPTimeWaitSource = "combined"
	} else {
		out.TCPTimeWaitSource = mostCommonString(snapshotTCPTimeWaitSources(samples))
	}
	out.CollectionErrors = appendUniqueStrings(nil, errors...)
	return out
}

func addSocketStats(a, b SocketStats) SocketStats {
	return SocketStats{
		SocketsUsed: a.SocketsUsed + b.SocketsUsed,
		TCPInUse:    a.TCPInUse + b.TCPInUse,
		TCPOrphan:   a.TCPOrphan + b.TCPOrphan,
		TCPTimeWait: a.TCPTimeWait + b.TCPTimeWait,
		TCPAlloc:    a.TCPAlloc + b.TCPAlloc,
		TCPMem:      a.TCPMem + b.TCPMem,
		UDPInUse:    a.UDPInUse + b.UDPInUse,
		UDPMem:      a.UDPMem + b.UDPMem,
		RAWInUse:    a.RAWInUse + b.RAWInUse,
		FRAGInUse:   a.FRAGInUse + b.FRAGInUse,
		FRAGMemory:  a.FRAGMemory + b.FRAGMemory,
	}
}

func addTCPTableStats(a, b TCPTableStats) TCPTableStats {
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

func aggregateSocketStats(samples []SocketStats) SocketStats {
	return SocketStats{
		SocketsUsed: medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.SocketsUsed }),
		TCPInUse:    medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.TCPInUse }),
		TCPOrphan:   medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.TCPOrphan }),
		TCPTimeWait: medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.TCPTimeWait }),
		TCPAlloc:    medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.TCPAlloc }),
		TCPMem:      medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.TCPMem }),
		UDPInUse:    medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.UDPInUse }),
		UDPMem:      medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.UDPMem }),
		RAWInUse:    medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.RAWInUse }),
		FRAGInUse:   medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.FRAGInUse }),
		FRAGMemory:  medianSocketUint64(samples, func(sample SocketStats) uint64 { return sample.FRAGMemory }),
	}
}

func aggregateTCPTableStats(samples []TCPTableStats) TCPTableStats {
	return TCPTableStats{
		Entries:     medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.Entries }),
		Established: medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.Established }),
		SynSent:     medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.SynSent }),
		SynRecv:     medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.SynRecv }),
		FinWait1:    medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.FinWait1 }),
		FinWait2:    medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.FinWait2 }),
		TimeWait:    medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.TimeWait }),
		Close:       medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.Close }),
		CloseWait:   medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.CloseWait }),
		LastAck:     medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.LastAck }),
		Listen:      medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.Listen }),
		Closing:     medianTCPTableUint64(samples, func(sample TCPTableStats) uint64 { return sample.Closing }),
	}
}

func latestClientResourceCollectedAt(samples []ClientResourceSnapshot) time.Time {
	var latest time.Time
	for _, sample := range samples {
		if sample.CollectedAt.After(latest) {
			latest = sample.CollectedAt
		}
	}
	return latest
}

func snapshotSockstats(samples []ClientResourceSnapshot) []SocketStats {
	out := make([]SocketStats, 0, len(samples))
	for _, sample := range samples {
		out = append(out, sample.Sockstat)
	}
	return out
}

func snapshotTCPTables(samples []ClientResourceSnapshot) []TCPTableStats {
	out := make([]TCPTableStats, 0, len(samples))
	for _, sample := range samples {
		out = append(out, sample.TCPTable)
	}
	return out
}

func snapshotTCPTimeWaitSources(samples []ClientResourceSnapshot) []string {
	out := make([]string, 0, len(samples))
	for _, sample := range samples {
		out = append(out, sample.TCPTimeWaitSource)
	}
	return out
}

func medianSnapshotUint64(samples []ClientResourceSnapshot, get func(ClientResourceSnapshot) uint64) uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, get(sample))
	}
	return medianUint64(values)
}

func medianSocketUint64(samples []SocketStats, get func(SocketStats) uint64) uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, get(sample))
	}
	return medianUint64(values)
}

func medianTCPTableUint64(samples []TCPTableStats, get func(TCPTableStats) uint64) uint64 {
	values := make([]uint64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, get(sample))
	}
	return medianUint64(values)
}

func medianDeltaInt64(samples []ClientResourceDelta, get func(ClientResourceDelta) int64) int64 {
	values := make([]int64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, get(sample))
	}
	return medianInt64(values)
}
