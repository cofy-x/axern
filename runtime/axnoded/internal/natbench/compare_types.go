package natbench

import "github.com/cofy-x/axern/network/bpfnet"

type ComparisonDatapath struct {
	ThroughputRPS     float64                `json:"throughputRps"`
	P50Ms             float64                `json:"p50Ms"`
	P95Ms             float64                `json:"p95Ms"`
	P99Ms             float64                `json:"p99Ms"`
	HostCPUPercent    float64                `json:"hostCpuPercent"`
	TotalRequests     int                    `json:"totalRequests,omitempty"`
	TotalSuccesses    int                    `json:"totalSuccesses,omitempty"`
	TotalFailures     int                    `json:"totalFailures,omitempty"`
	FailureRate       float64                `json:"failureRate,omitempty"`
	RunsWithFailures  int                    `json:"runsWithFailures,omitempty"`
	MaxFailures       int                    `json:"maxFailures,omitempty"`
	FirstError        string                 `json:"firstError,omitempty"`
	Datapath          string                 `json:"datapath"`
	Mode              string                 `json:"mode,omitempty"`
	Profile           NATProfile             `json:"profile,omitempty"`
	ClientAfter       ClientResourceSnapshot `json:"clientAfter,omitempty"`
	ClientPeak        ClientResourceSnapshot `json:"clientPeak,omitempty"`
	ClientDelta       ClientResourceDelta    `json:"clientDelta,omitempty"`
	ClientPeakDelta   ClientResourceDelta    `json:"clientPeakDelta,omitempty"`
	ClientSamples     uint64                 `json:"clientSamples,omitempty"`
	KernelBefore      bpfnet.KernelStats     `json:"kernelBefore,omitempty"`
	KernelAfter       bpfnet.KernelStats     `json:"kernelAfter,omitempty"`
	KernelDelta       bpfnet.KernelStats     `json:"kernelDelta,omitempty"`
	SNATMapAfter      bpfnet.SNATMapStats    `json:"snatMapAfter,omitempty"`
	SNATMapDelta      bpfnet.SNATMapStats    `json:"snatMapDelta,omitempty"`
	SNATMapPeak       bpfnet.SNATMapStats    `json:"snatMapPeak,omitempty"`
	SNATMapPeakDelta  bpfnet.SNATMapStats    `json:"snatMapPeakDelta,omitempty"`
	SNATMapSamples    uint64                 `json:"snatMapSamples,omitempty"`
	SNATMapPostGC     bpfnet.SNATMapStats    `json:"snatMapPostGc,omitempty"`
	SNATMapGCReleased bpfnet.SNATMapStats    `json:"snatMapGcReleased,omitempty"`
	SampleCount       int                    `json:"sampleCount,omitempty"`
}

type ComparisonDelta struct {
	ThroughputPct *float64 `json:"throughputPct"`
	P95Pct        *float64 `json:"p95Pct"`
	HostCPUPct    *float64 `json:"hostCpuPct"`
}

type ComparisonPath struct {
	Name     string             `json:"name"`
	Iptables ComparisonDatapath `json:"iptables"`
	EBPF     ComparisonDatapath `json:"ebpf"`
	Delta    ComparisonDelta    `json:"delta"`
}

type AggregationInfo struct {
	IptablesRuns int    `json:"iptablesRuns"`
	EBPFRuns     int    `json:"ebpfRuns"`
	Method       string `json:"method"`
}

type CompareReport struct {
	Iptables    Report           `json:"iptables"`
	EBPF        Report           `json:"ebpf"`
	Comparison  []ComparisonPath `json:"comparison"`
	Aggregation AggregationInfo  `json:"aggregation"`
}
