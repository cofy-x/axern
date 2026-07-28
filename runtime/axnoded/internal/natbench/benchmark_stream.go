package natbench

const (
	BenchmarkPhaseBeginEvent = "benchmark-phase-begin"
	BenchmarkPhaseEndEvent   = "benchmark-phase-end"
)

type BenchmarkStreamEvent struct {
	Event         string                  `json:"event"`
	Transport     string                  `json:"transport,omitempty"`
	Summary       *WorkloadSummary        `json:"summary,omitempty"`
	Client        *ClientResourceSnapshot `json:"client,omitempty"`
	ClientPeak    *ClientResourceSnapshot `json:"clientPeak,omitempty"`
	ClientSamples uint64                  `json:"clientSamples,omitempty"`
}
