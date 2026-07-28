package natbench

type DurationQuantiles struct {
	P50Seconds float64 `json:"p50Seconds,omitempty"`
	P95Seconds float64 `json:"p95Seconds,omitempty"`
	P99Seconds float64 `json:"p99Seconds,omitempty"`
}

type HistogramBucketSnapshot struct {
	UpperBound      float64 `json:"upperBound"`
	CumulativeCount uint64  `json:"cumulativeCount"`
}

type HistogramSnapshot struct {
	Count             uint64                    `json:"count,omitempty"`
	SumSeconds        float64                   `json:"sumSeconds,omitempty"`
	Buckets           []HistogramBucketSnapshot `json:"buckets,omitempty"`
	SampleStart       uint64                    `json:"sampleStart,omitempty"`
	SamplesCumulative bool                      `json:"samplesCumulative,omitempty"`
	Samples           []float64                 `json:"samples,omitempty"`
}

type StartupSummary struct {
	Runtime          string                                      `json:"runtime"`
	RootfsType       string                                      `json:"rootfsType"`
	Classes          map[string]StartupClassSummary              `json:"classes,omitempty"`
	Phases           map[string]StartupPhaseSummary              `json:"phases,omitempty"`
	PhaseBreakdown   map[string]map[string]StartupPhaseBreakdown `json:"phaseBreakdown,omitempty"`
	DominantPhaseP95 map[string]string                           `json:"dominantPhaseP95,omitempty"`
	DominantPhaseP99 map[string]string                           `json:"dominantPhaseP99,omitempty"`
	Bundle           *BundleTemplateSummary                      `json:"bundle,omitempty"`
	Envelope         *ExecutionEnvelopeSummary                   `json:"executionEnvelope,omitempty"`
	WaitGrace        *RuntimeWaitGraceSummary                    `json:"waitGrace,omitempty"`
}

type StartupClassSummary struct {
	OKCount                uint64             `json:"okCount,omitempty"`
	ErrorCount             uint64             `json:"errorCount,omitempty"`
	AverageDurationSeconds float64            `json:"averageDurationSeconds,omitempty"`
	Quantiles              *DurationQuantiles `json:"quantiles,omitempty"`
	Histogram              *HistogramSnapshot `json:"histogram,omitempty"`
}

type StartupSnapshot struct {
	Runtime    string                          `json:"runtime"`
	RootfsType string                          `json:"rootfsType"`
	Classes    map[string]StartupClassSnapshot `json:"classes,omitempty"`
	Phases     map[string]StartupPhaseSnapshot `json:"phases,omitempty"`
	Bundle     *BundleTemplateSnapshot         `json:"bundle,omitempty"`
	Envelope   *ExecutionEnvelopeSnapshot      `json:"executionEnvelope,omitempty"`
	WaitGrace  *RuntimeWaitGraceSnapshot       `json:"waitGrace,omitempty"`
}

type StartupClassSnapshot struct {
	OKCount              uint64             `json:"okCount,omitempty"`
	ErrorCount           uint64             `json:"errorCount,omitempty"`
	OKDurationCount      uint64             `json:"okDurationCount,omitempty"`
	OKDurationSumSeconds float64            `json:"okDurationSumSeconds,omitempty"`
	Histogram            *HistogramSnapshot `json:"histogram,omitempty"`
}

type StartupPhaseSummary struct {
	Classes map[string]StartupPhaseClassSummary `json:"classes,omitempty"`
}

type StartupPhaseClassSummary struct {
	Count                  uint64             `json:"count,omitempty"`
	ErrorCount             uint64             `json:"errorCount,omitempty"`
	AverageDurationSeconds float64            `json:"averageDurationSeconds,omitempty"`
	Quantiles              *DurationQuantiles `json:"quantiles,omitempty"`
	Histogram              *HistogramSnapshot `json:"histogram,omitempty"`
}

type StartupPhaseBreakdown struct {
	Count      uint64  `json:"count,omitempty"`
	ErrorCount uint64  `json:"errorCount,omitempty"`
	P50Seconds float64 `json:"p50Seconds,omitempty"`
	P95Seconds float64 `json:"p95Seconds,omitempty"`
	P99Seconds float64 `json:"p99Seconds,omitempty"`
}

type StartupPhaseSnapshot struct {
	Classes map[string]StartupPhaseClassSnapshot `json:"classes,omitempty"`
}

type StartupPhaseClassSnapshot struct {
	Count      uint64             `json:"count,omitempty"`
	ErrorCount uint64             `json:"errorCount,omitempty"`
	SumSeconds float64            `json:"sumSeconds,omitempty"`
	Histogram  *HistogramSnapshot `json:"histogram,omitempty"`
}

type BundleTemplateSummary struct {
	HitCount                      uint64             `json:"hitCount,omitempty"`
	MissCount                     uint64             `json:"missCount,omitempty"`
	ErrorCount                    uint64             `json:"errorCount,omitempty"`
	HitRate                       float64            `json:"hitRate,omitempty"`
	AverageMaterializeDurationSec float64            `json:"averageMaterializeDurationSeconds,omitempty"`
	MaterializeQuantiles          *DurationQuantiles `json:"materializeQuantiles,omitempty"`
	MaterializeHistogram          *HistogramSnapshot `json:"materializeHistogram,omitempty"`
}

type BundleTemplateSnapshot struct {
	HitCount              uint64             `json:"hitCount,omitempty"`
	MissCount             uint64             `json:"missCount,omitempty"`
	ErrorCount            uint64             `json:"errorCount,omitempty"`
	MaterializeCount      uint64             `json:"materializeCount,omitempty"`
	MaterializeSumSeconds float64            `json:"materializeSumSeconds,omitempty"`
	MaterializeHistogram  *HistogramSnapshot `json:"materializeHistogram,omitempty"`
}

type ExecutionEnvelopeSummary struct {
	PreparedCount              uint64             `json:"preparedCount,omitempty"`
	HitCount                   uint64             `json:"hitCount,omitempty"`
	MissCount                  uint64             `json:"missCount,omitempty"`
	ErrorCount                 uint64             `json:"errorCount,omitempty"`
	FallbackCount              uint64             `json:"fallbackCount,omitempty"`
	AveragePrepareDurationSec  float64            `json:"averagePrepareDurationSeconds,omitempty"`
	PrepareQuantiles           *DurationQuantiles `json:"prepareQuantiles,omitempty"`
	PrepareHistogram           *HistogramSnapshot `json:"prepareHistogram,omitempty"`
	AverageActivateDurationSec float64            `json:"averageActivateDurationSeconds,omitempty"`
	ActivateQuantiles          *DurationQuantiles `json:"activateQuantiles,omitempty"`
	ActivateHistogram          *HistogramSnapshot `json:"activateHistogram,omitempty"`
}

type ExecutionEnvelopeSnapshot struct {
	PreparedCount     uint64             `json:"preparedCount,omitempty"`
	HitCount          uint64             `json:"hitCount,omitempty"`
	MissCount         uint64             `json:"missCount,omitempty"`
	ErrorCount        uint64             `json:"errorCount,omitempty"`
	FallbackCount     uint64             `json:"fallbackCount,omitempty"`
	PrepareHistogram  *HistogramSnapshot `json:"prepareHistogram,omitempty"`
	ActivateHistogram *HistogramSnapshot `json:"activateHistogram,omitempty"`
}

type RuntimeWaitGraceSummary struct {
	RecoveredCount   uint64 `json:"recoveredCount,omitempty"`
	UnavailableCount uint64 `json:"unavailableCount,omitempty"`
}

type RuntimeWaitGraceSnapshot struct {
	RecoveredCount   uint64 `json:"recoveredCount,omitempty"`
	UnavailableCount uint64 `json:"unavailableCount,omitempty"`
}
