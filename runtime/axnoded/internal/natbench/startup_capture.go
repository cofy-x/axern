package natbench

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	axmetrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
)

var metricsSnapshotClient = &http.Client{Timeout: 5 * time.Second}

func CaptureStartupSummary(metricsURL, runtimeName, rootfsType string) (*StartupSummary, error) {
	snapshot, err := CaptureStartupSnapshot(metricsURL, runtimeName, rootfsType)
	if err != nil {
		return nil, err
	}
	return DiffStartupSummary(nil, snapshot), nil
}

func CaptureStartupSnapshot(metricsURL, runtimeName, rootfsType string) (*StartupSnapshot, error) {
	if metricsURL == "" {
		return nil, nil
	}

	snapshot, err := fetchMetricsSnapshot(metricsURL)
	if err != nil {
		return nil, err
	}

	out := &StartupSnapshot{
		Runtime:    runtimeName,
		RootfsType: rootfsType,
		Classes:    map[string]StartupClassSnapshot{},
		Phases:     map[string]StartupPhaseSnapshot{},
	}
	collectStartupCounterSnapshot(out, snapshot.Points, runtimeName, rootfsType)
	collectStartupDurationSnapshot(out, snapshot.Points, runtimeName, rootfsType)
	collectStartupPhaseSnapshot(out, snapshot.Points, runtimeName, rootfsType)
	collectBundleTemplateSnapshot(out, snapshot.Points, runtimeName, rootfsType)
	collectBundleMaterializeSnapshot(out, snapshot.Points, runtimeName, rootfsType)
	collectExecutionEnvelopeSnapshot(out, snapshot.Points, runtimeName, rootfsType)
	collectExecutionEnvelopePrepareSnapshot(out, snapshot.Points, runtimeName, rootfsType)
	collectExecutionEnvelopeActivateSnapshot(out, snapshot.Points, runtimeName, rootfsType)
	collectRuntimeWaitGraceSnapshot(out, snapshot.Points, runtimeName)

	if len(out.Classes) == 0 && len(out.Phases) == 0 && out.Bundle == nil && out.Envelope == nil && out.WaitGrace == nil {
		return nil, nil
	}
	return out, nil
}

func fetchMetricsSnapshot(metricsURL string) (*axmetrics.Snapshot, error) {
	resp, err := metricsSnapshotClient.Get(metricsURL)
	if err != nil {
		return nil, fmt.Errorf("fetch metrics snapshot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch metrics snapshot: unexpected status %d", resp.StatusCode)
	}
	var snapshot axmetrics.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode metrics snapshot: %w", err)
	}
	if snapshot.Version != axmetrics.SnapshotVersion {
		return nil, fmt.Errorf("decode metrics snapshot: unsupported version %q", snapshot.Version)
	}
	if snapshot.DroppedRecords != 0 {
		return nil, fmt.Errorf("decode metrics snapshot: %d debug metric records were dropped", snapshot.DroppedRecords)
	}
	return &snapshot, nil
}

func collectStartupCounterSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName, rootfsType string) {
	for _, point := range points {
		if point.Name != axmetrics.MetricStartupTotal || point.Type != axmetrics.TypeCounter {
			continue
		}
		if !hasRuntimeRootfs(point, runtimeName, rootfsType) {
			continue
		}
		startClass := point.Attributes[sdkobs.AttrStartClass]
		result := point.Attributes[sdkobs.AttrResult]
		if startClass == "" || result == "" {
			continue
		}

		classSummary := snapshot.Classes[startClass]
		switch result {
		case "ok":
			classSummary.OKCount += roundedCounter(point.Value)
		case "error":
			classSummary.ErrorCount += roundedCounter(point.Value)
		}
		snapshot.Classes[startClass] = classSummary
	}
}

func collectStartupDurationSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName, rootfsType string) {
	for _, point := range points {
		if point.Name != axmetrics.MetricStartupDuration || point.Type != axmetrics.TypeHistogram {
			continue
		}
		if !hasRuntimeRootfs(point, runtimeName, rootfsType) || point.Attributes[sdkobs.AttrResult] != "ok" {
			continue
		}
		startClass := point.Attributes[sdkobs.AttrStartClass]
		if startClass == "" || point.Count == 0 {
			continue
		}

		classSummary := snapshot.Classes[startClass]
		classSummary.OKDurationCount = point.Count
		classSummary.OKDurationSumSeconds = point.Sum
		classSummary.Histogram = histogramFromPoint(point.Count, point.Sum, point.SampleStart, point.Samples)
		snapshot.Classes[startClass] = classSummary
	}
}

func collectStartupPhaseSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName, rootfsType string) {
	if snapshot.Phases == nil {
		snapshot.Phases = map[string]StartupPhaseSnapshot{}
	}
	for _, point := range points {
		if point.Name != axmetrics.MetricStartupPhaseDuration || point.Type != axmetrics.TypeHistogram {
			continue
		}
		if !hasRuntimeRootfs(point, runtimeName, rootfsType) {
			continue
		}
		phase := point.Attributes[sdkobs.AttrPhase]
		startClass := point.Attributes[sdkobs.AttrStartClass]
		result := point.Attributes[sdkobs.AttrResult]
		if phase == "" || startClass == "" {
			continue
		}

		phaseSnapshot := snapshot.Phases[phase]
		if phaseSnapshot.Classes == nil {
			phaseSnapshot.Classes = map[string]StartupPhaseClassSnapshot{}
		}
		classSummary := phaseSnapshot.Classes[startClass]
		classSummary.Count += point.Count
		classSummary.SumSeconds += point.Sum
		classSummary.Histogram = mergeHistogram(classSummary.Histogram, histogramFromPoint(point.Count, point.Sum, point.SampleStart, point.Samples))
		if result == "error" {
			classSummary.ErrorCount += point.Count
		}
		phaseSnapshot.Classes[startClass] = classSummary
		snapshot.Phases[phase] = phaseSnapshot
	}
}

func collectBundleTemplateSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName, rootfsType string) {
	bundle := snapshot.Bundle
	for _, point := range points {
		if point.Name != axmetrics.MetricBundleTemplateTotal || point.Type != axmetrics.TypeCounter {
			continue
		}
		if !hasRuntimeRootfs(point, runtimeName, rootfsType) {
			continue
		}
		if bundle == nil {
			bundle = &BundleTemplateSnapshot{}
		}
		switch point.Attributes[sdkobs.AttrResult] {
		case "hit":
			bundle.HitCount += roundedCounter(point.Value)
		case "miss":
			bundle.MissCount += roundedCounter(point.Value)
		case "error":
			bundle.ErrorCount += roundedCounter(point.Value)
		}
	}
	if bundle == nil || (bundle.HitCount == 0 && bundle.MissCount == 0 && bundle.ErrorCount == 0) {
		return
	}
	snapshot.Bundle = bundle
}

func collectBundleMaterializeSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName, rootfsType string) {
	bundle := snapshot.Bundle
	for _, point := range points {
		if point.Name != axmetrics.MetricBundleMaterializeDuration || point.Type != axmetrics.TypeHistogram {
			continue
		}
		if !hasRuntimeRootfs(point, runtimeName, rootfsType) || point.Attributes[sdkobs.AttrResult] != "ok" {
			continue
		}
		if bundle == nil {
			bundle = &BundleTemplateSnapshot{}
		}
		bundle.MaterializeCount += point.Count
		bundle.MaterializeSumSeconds += point.Sum
		bundle.MaterializeHistogram = mergeHistogram(bundle.MaterializeHistogram, histogramFromPoint(point.Count, point.Sum, point.SampleStart, point.Samples))
	}
	if bundle == nil || (bundle.HitCount == 0 && bundle.MissCount == 0 && bundle.ErrorCount == 0 && bundle.MaterializeCount == 0) {
		return
	}
	snapshot.Bundle = bundle
}

func collectExecutionEnvelopeSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName, rootfsType string) {
	envelope := snapshot.Envelope
	for _, point := range points {
		if point.Name != axmetrics.MetricExecutionEnvelopeTotal || point.Type != axmetrics.TypeCounter {
			continue
		}
		if !hasRuntimeRootfs(point, runtimeName, rootfsType) {
			continue
		}
		if envelope == nil {
			envelope = &ExecutionEnvelopeSnapshot{}
		}
		switch point.Attributes[sdkobs.AttrResult] {
		case "prepared":
			envelope.PreparedCount += roundedCounter(point.Value)
		case "hit":
			envelope.HitCount += roundedCounter(point.Value)
		case "miss":
			envelope.MissCount += roundedCounter(point.Value)
		case "error":
			envelope.ErrorCount += roundedCounter(point.Value)
		case "fallback":
			envelope.FallbackCount += roundedCounter(point.Value)
		}
	}
	if envelope == nil || (envelope.PreparedCount == 0 && envelope.HitCount == 0 && envelope.MissCount == 0 && envelope.ErrorCount == 0 && envelope.FallbackCount == 0) {
		return
	}
	snapshot.Envelope = envelope
}

func collectExecutionEnvelopePrepareSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName, rootfsType string) {
	envelope := snapshot.Envelope
	for _, point := range points {
		if point.Name != axmetrics.MetricExecutionEnvelopePrepare || point.Type != axmetrics.TypeHistogram {
			continue
		}
		if !hasRuntimeRootfs(point, runtimeName, rootfsType) || point.Attributes[sdkobs.AttrResult] != "ok" {
			continue
		}
		if envelope == nil {
			envelope = &ExecutionEnvelopeSnapshot{}
		}
		envelope.PrepareHistogram = mergeHistogram(envelope.PrepareHistogram, histogramFromPoint(point.Count, point.Sum, point.SampleStart, point.Samples))
	}
	if envelope == nil || (envelope.PreparedCount == 0 && envelope.HitCount == 0 && envelope.MissCount == 0 && envelope.ErrorCount == 0 && envelope.FallbackCount == 0 && envelope.PrepareHistogram == nil) {
		return
	}
	snapshot.Envelope = envelope
}

func collectExecutionEnvelopeActivateSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName, rootfsType string) {
	envelope := snapshot.Envelope
	for _, point := range points {
		if point.Name != axmetrics.MetricExecutionEnvelopeActivate || point.Type != axmetrics.TypeHistogram {
			continue
		}
		if !hasRuntimeRootfs(point, runtimeName, rootfsType) || point.Attributes[sdkobs.AttrResult] != "ok" {
			continue
		}
		if envelope == nil {
			envelope = &ExecutionEnvelopeSnapshot{}
		}
		envelope.ActivateHistogram = mergeHistogram(envelope.ActivateHistogram, histogramFromPoint(point.Count, point.Sum, point.SampleStart, point.Samples))
	}
	if envelope == nil || (envelope.PreparedCount == 0 && envelope.HitCount == 0 && envelope.MissCount == 0 && envelope.ErrorCount == 0 && envelope.FallbackCount == 0 && envelope.PrepareHistogram == nil && envelope.ActivateHistogram == nil) {
		return
	}
	snapshot.Envelope = envelope
}

func collectRuntimeWaitGraceSnapshot(snapshot *StartupSnapshot, points []axmetrics.Point, runtimeName string) {
	waitGrace := &RuntimeWaitGraceSnapshot{}
	for _, point := range points {
		if point.Name != axmetrics.MetricRuntimeWaitGraceTotal || point.Type != axmetrics.TypeCounter {
			continue
		}
		if point.Attributes[sdkobs.AttrRuntime] != runtimeName {
			continue
		}
		switch point.Attributes[sdkobs.AttrResult] {
		case "recovered":
			waitGrace.RecoveredCount += roundedCounter(point.Value)
		case "unavailable":
			waitGrace.UnavailableCount += roundedCounter(point.Value)
		}
	}
	if waitGrace.RecoveredCount == 0 && waitGrace.UnavailableCount == 0 {
		return
	}
	snapshot.WaitGrace = waitGrace
}

func hasRuntimeRootfs(point axmetrics.Point, runtimeName, rootfsType string) bool {
	return point.Attributes[sdkobs.AttrRuntime] == runtimeName &&
		point.Attributes[sdkobs.AttrRootFSType] == rootfsType
}

func roundedCounter(value float64) uint64 {
	return uint64(value + 0.5)
}
