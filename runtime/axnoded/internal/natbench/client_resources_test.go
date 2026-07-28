package natbench

import (
	"testing"
	"time"
)

func TestParseEphemeralPortRange(t *testing.T) {
	first, last, err := parseEphemeralPortRange("32768\t60999\n")
	if err != nil {
		t.Fatalf("parseEphemeralPortRange returned error: %v", err)
	}
	if first != 32768 || last != 60999 {
		t.Fatalf("unexpected range: %d-%d", first, last)
	}
}

func TestParseEphemeralPortRangeRejectsInvertedRange(t *testing.T) {
	_, _, err := parseEphemeralPortRange("60999 32768")
	if err == nil {
		t.Fatal("expected invalid range error")
	}
}

func TestParseSockstat(t *testing.T) {
	stats, err := parseSockstat(`
sockets: used 1024
TCP: inuse 4 orphan 1 tw 128 alloc 10 mem 3
UDP: inuse 2 mem 5
UDPLITE: inuse 0
RAW: inuse 1
FRAG: inuse 6 memory 7
`)
	if err != nil {
		t.Fatalf("parseSockstat returned error: %v", err)
	}
	if stats.SocketsUsed != 1024 || stats.TCPInUse != 4 || stats.TCPOrphan != 1 || stats.TCPTimeWait != 128 || stats.TCPAlloc != 10 || stats.TCPMem != 3 {
		t.Fatalf("unexpected tcp socket stats: %#v", stats)
	}
	if stats.UDPInUse != 2 || stats.UDPMem != 5 || stats.RAWInUse != 1 || stats.FRAGInUse != 6 || stats.FRAGMemory != 7 {
		t.Fatalf("unexpected non-tcp socket stats: %#v", stats)
	}
}

func TestParseTCPTable(t *testing.T) {
	stats, err := parseTCPTable(`
  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000   100        0 123 1 0000000000000000 100 0 0 10 0
   1: 0200007F:CAFE 0300007F:0050 01 00000000:00000000 00:00000000 00000000   100        0 124 1 0000000000000000 20 4 30 10 -1
   2: 0200007F:BEEF 0300007F:0050 06 00000000:00000000 03:00001770 00000000   100        0 0 3 0000000000000000
`)
	if err != nil {
		t.Fatalf("parseTCPTable returned error: %v", err)
	}
	if stats.Entries != 3 || stats.Listen != 1 || stats.Established != 1 || stats.TimeWait != 1 {
		t.Fatalf("unexpected tcp table stats: %#v", stats)
	}
}

func TestClientResourcesDelta(t *testing.T) {
	before := ClientResourceSnapshot{
		Sockstat: SocketStats{
			SocketsUsed: 100,
			TCPInUse:    10,
			TCPTimeWait: 20,
			TCPAlloc:    30,
		},
	}
	after := ClientResourceSnapshot{
		Sockstat: SocketStats{
			SocketsUsed: 97,
			TCPInUse:    11,
			TCPTimeWait: 45,
			TCPAlloc:    35,
		},
		TCPTable: TCPTableStats{
			Entries:     64,
			Established: 64,
		},
	}

	delta := ClientResourcesDelta(before, after)
	if delta.SocketsUsed != -3 || delta.TCPInUse != 1 || delta.TCPTimeWait != 25 || delta.TCPAlloc != 5 {
		t.Fatalf("unexpected client resource delta: %#v", delta)
	}
	if delta.TCPTableEntries != 64 || delta.TCPTableEstablished != 64 {
		t.Fatalf("unexpected tcp table delta: %#v", delta)
	}
}

func TestClientResourcesDeltaUsesTCPTableFallback(t *testing.T) {
	before := ClientResourceSnapshot{
		TCPTimeWaitCount:  3,
		TCPTimeWaitSource: "tcp_table",
		TCPTable: TCPTableStats{
			TimeWait: 3,
		},
	}
	after := ClientResourceSnapshot{
		TCPTimeWaitCount:  11,
		TCPTimeWaitSource: "tcp_table",
		TCPTable: TCPTableStats{
			TimeWait: 11,
		},
	}

	delta := ClientResourcesDelta(before, after)
	if delta.TCPTimeWait != 8 {
		t.Fatalf("expected tcp table time-wait delta, got %#v", delta)
	}
}

func TestMergeClientResourcePeakKeepsIndependentHighWaterMarks(t *testing.T) {
	first := ClientResourceSnapshot{
		CollectedAt:          time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC),
		OpenFileLimit:        1024,
		EphemeralPortFirst:   32768,
		EphemeralPortLast:    60999,
		EphemeralPortCount:   28232,
		TCPTimeWaitCount:     3,
		TCPTimeWaitSource:    "tcp_table",
		CollectionErrors:     []string{"read tcp_tw_reuse: denied"},
		TCPFinTimeoutSeconds: 60,
		TCPTable: TCPTableStats{
			Entries: 10,
		},
	}
	second := ClientResourceSnapshot{
		CollectedAt:        first.CollectedAt.Add(time.Second),
		OpenFileLimit:      2048,
		EphemeralPortFirst: 49152,
		EphemeralPortLast:  65000,
		EphemeralPortCount: 15849,
		TCPTimeWaitCount:   4,
		TCPTimeWaitSource:  "tcp_table",
		CollectionErrors:   []string{"read tcp_tw_reuse: denied", "read sockstat: denied"},
		TCPTable: TCPTableStats{
			Entries: 1,
			Listen:  2,
		},
	}
	peak := mergeClientResourcePeak(first, second)
	if peak.TCPTimeWaitCount != 4 || peak.TCPTable.Entries != 10 || peak.TCPTable.Listen != 2 {
		t.Fatalf("expected independent peak counters, got %#v", peak)
	}
	if peak.OpenFileLimit != 2048 || peak.EphemeralPortFirst != 32768 || peak.EphemeralPortLast != 65000 || peak.EphemeralPortCount != 28232 {
		t.Fatalf("expected peak limit and range values, got %#v", peak)
	}
	if !peak.CollectedAt.Equal(second.CollectedAt) {
		t.Fatalf("expected latest collection time, got %s", peak.CollectedAt)
	}
	if len(peak.CollectionErrors) != 2 {
		t.Fatalf("expected deduped collection errors, got %#v", peak.CollectionErrors)
	}
}

func TestAggregateClientResourceSnapshotsUsesMedian(t *testing.T) {
	baseTime := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	samples := []ClientResourceSnapshot{
		{
			CollectedAt:          baseTime,
			OpenFileLimit:        1024,
			EphemeralPortFirst:   32768,
			EphemeralPortLast:    60999,
			EphemeralPortCount:   28232,
			TCPTimeWaitReuse:     2,
			TCPFinTimeoutSeconds: 60,
			TCPTimeWaitCount:     100,
			TCPTimeWaitSource:    "sockstat",
			Sockstat: SocketStats{
				TCPTimeWait: 100,
			},
			CollectionErrors: []string{"read tcp_tw_reuse: denied"},
		},
		{
			CollectedAt:        baseTime.Add(time.Second),
			OpenFileLimit:      4096,
			EphemeralPortFirst: 32768,
			EphemeralPortLast:  60999,
			EphemeralPortCount: 28232,
			TCPTimeWaitCount:   300,
			TCPTimeWaitSource:  "sockstat",
			Sockstat: SocketStats{
				TCPTimeWait: 300,
			},
		},
		{
			CollectedAt:        baseTime.Add(2 * time.Second),
			OpenFileLimit:      2048,
			EphemeralPortFirst: 32768,
			EphemeralPortLast:  60999,
			EphemeralPortCount: 28232,
			TCPTimeWaitCount:   200,
			TCPTimeWaitSource:  "sockstat",
			Sockstat: SocketStats{
				TCPTimeWait: 200,
			},
			CollectionErrors: []string{"read tcp_tw_reuse: denied"},
		},
	}

	aggregated := aggregateClientResourceSnapshots(samples)
	if aggregated.OpenFileLimit != 2048 || aggregated.Sockstat.TCPTimeWait != 200 {
		t.Fatalf("expected median resource snapshot, got %#v", aggregated)
	}
	if aggregated.TCPTimeWaitCount != 200 || aggregated.TCPTimeWaitSource != "sockstat" {
		t.Fatalf("expected aggregated time-wait count, got %#v", aggregated)
	}
	if !aggregated.CollectedAt.Equal(baseTime.Add(2 * time.Second)) {
		t.Fatalf("expected latest collection time, got %s", aggregated.CollectedAt)
	}
	if len(aggregated.CollectionErrors) != 1 {
		t.Fatalf("expected deduped collection errors, got %#v", aggregated.CollectionErrors)
	}
}

func TestCombineClientResourceSnapshotsSumsClientNamespaces(t *testing.T) {
	baseTime := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	combined := CombineClientResourceSnapshots([]ClientResourceSnapshot{
		{
			CollectedAt:        baseTime,
			OpenFileLimit:      1024,
			EphemeralPortFirst: 32768,
			EphemeralPortLast:  60999,
			EphemeralPortCount: 28232,
			TCPTimeWaitCount:   10,
			TCPTimeWaitSource:  "tcp_table",
			Sockstat: SocketStats{
				SocketsUsed: 5,
			},
			TCPTable: TCPTableStats{
				Entries:     64,
				Established: 64,
			},
			CollectionErrors: []string{"read sockstat: denied"},
		},
		{
			CollectedAt:        baseTime.Add(time.Second),
			OpenFileLimit:      2048,
			EphemeralPortFirst: 49152,
			EphemeralPortLast:  65000,
			EphemeralPortCount: 15849,
			TCPTimeWaitCount:   20,
			TCPTimeWaitSource:  "tcp_table",
			Sockstat: SocketStats{
				SocketsUsed: 7,
			},
			TCPTable: TCPTableStats{
				Entries:     128,
				Established: 127,
			},
			CollectionErrors: []string{"read sockstat: denied"},
		},
	})

	if combined.OpenFileLimit != 3072 || combined.EphemeralPortFirst != 32768 || combined.EphemeralPortLast != 65000 || combined.EphemeralPortCount != 44081 {
		t.Fatalf("unexpected combined limits: %#v", combined)
	}
	if combined.TCPTimeWaitCount != 30 || combined.TCPTimeWaitSource != "combined" {
		t.Fatalf("unexpected combined time-wait: %#v", combined)
	}
	if combined.Sockstat.SocketsUsed != 12 || combined.TCPTable.Entries != 192 || combined.TCPTable.Established != 191 {
		t.Fatalf("unexpected combined dynamic counters: %#v", combined)
	}
	if !combined.CollectedAt.Equal(baseTime.Add(time.Second)) || len(combined.CollectionErrors) != 1 {
		t.Fatalf("unexpected combined metadata: %#v", combined)
	}
}
