package natbench

import (
	"testing"
	"time"
)

func TestBuildStartupScenarioReportAggregatesColdAndWarmSamples(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	cold := StartupScenarioSampleReport{
		Scenario:    "runsc-local",
		Runtime:     "runsc",
		RootfsType:  "local",
		MountType:   "local",
		RootfsKey:   LocalRootfsKey("/opt/sample-rootfs"),
		Mode:        "cold",
		Samples:     1,
		Startup:     testStartupSummary("runsc", "local", "cold", "runtime_bundle_prepare", 0.12, 1, 1, 0),
		StartedAt:   now,
		CompletedAt: now.Add(time.Second),
	}
	warm := StartupScenarioSampleReport{
		Scenario:    "runsc-local",
		Runtime:     "runsc",
		RootfsType:  "local",
		MountType:   "local",
		RootfsKey:   LocalRootfsKey("/opt/sample-rootfs"),
		Mode:        "warm",
		Samples:     10,
		Startup:     testStartupSummary("runsc", "local", "warm", "runtime_launch", 0.03, 10, 9, 1),
		Locality:    &LocalitySummary{Key: LocalRootfsKey("/opt/sample-rootfs")},
		StartedAt:   now.Add(2 * time.Second),
		CompletedAt: now.Add(3 * time.Second),
	}

	report, err := BuildStartupScenarioReport([]StartupScenarioSampleReport{warm, cold})
	if err != nil {
		t.Fatalf("BuildStartupScenarioReport() error = %v", err)
	}
	if report.ColdSamples != 1 {
		t.Fatalf("cold samples = %d, want 1", report.ColdSamples)
	}
	if report.WarmSamples != 10 {
		t.Fatalf("warm samples = %d, want 10", report.WarmSamples)
	}
	if report.Startup == nil {
		t.Fatal("expected startup summary")
	}
	if report.Startup.DominantPhaseP95["warm"] != "runtime_launch" {
		t.Fatalf("warm dominant p95 = %q, want runtime_launch", report.Startup.DominantPhaseP95["warm"])
	}
	if report.Startup.Envelope == nil {
		t.Fatal("expected execution envelope summary to survive scenario aggregation")
	}
	if report.Locality == nil || report.Locality.Key == "" {
		t.Fatal("expected locality summary from warm sample")
	}
}

func TestBuildStartupMatrixReportProducesGateSummary(t *testing.T) {
	report := BuildStartupMatrixReport([]StartupScenarioReport{
		{
			Scenario:   "runsc-local",
			Runtime:    "runsc",
			RootfsType: "local",
			MountType:  "local",
			Startup:    testStartupSummary("runsc", "local", "warm", "runtime_launch", 0.03, 10, 9, 1),
		},
		{
			Scenario:   "runc-local",
			Runtime:    "runc",
			RootfsType: "local",
			MountType:  "local",
			Startup:    testStartupSummary("runc", "local", "warm", "runtime_launch", 0.02, 10, 8, 2),
		},
		{
			Scenario:   "runsc-oci",
			Runtime:    "runsc",
			RootfsType: "image",
			MountType:  "oci",
			Startup:    testStartupSummary("runsc", "image", "warm", "rootfs_prepare", 0.05, 10, 7, 3),
		},
	})

	if !report.GateSummary.RuntimeLaunchStillDominantAfterParity {
		t.Fatal("expected runtime launch dominant-after-parity gate to be true")
	}
	if report.GateSummary.ResourceAllocateStillDominant {
		t.Fatal("expected resource allocate dominant gate to be false")
	}
	if report.GateSummary.RuntimeBundlePrepareStillDominant {
		t.Fatal("expected runtime bundle prepare dominant gate to be false")
	}
	if !report.GateSummary.ImagePathDominant {
		t.Fatal("expected image path dominant gate to be true")
	}
	if !report.GateSummary.RuncEnvelopeParityAchieved {
		t.Fatal("expected runc envelope parity gate to be true")
	}
	if !report.GateSummary.HeavierLaunchMechanismCandidate {
		t.Fatal("expected heavier launch mechanism candidate gate to be true")
	}
	if !report.GateSummary.SnapshotRestoreStillDeferred {
		t.Fatal("expected snapshot restore still deferred gate to be true")
	}
}

func testStartupSummary(runtimeName, rootfsType, startClass, dominantPhase string, p95 float64, count uint64, hitCount, missCount uint64) *StartupSummary {
	phaseSummary := StartupPhaseClassSummary{
		Count: count,
		Quantiles: &DurationQuantiles{
			P50Seconds: p95 / 2,
			P95Seconds: p95,
			P99Seconds: p95 * 1.1,
		},
	}
	summary := &StartupSummary{
		Runtime:    runtimeName,
		RootfsType: rootfsType,
		Classes:    map[string]StartupClassSummary{},
		Phases: map[string]StartupPhaseSummary{
			dominantPhase: {
				Classes: map[string]StartupPhaseClassSummary{
					startClass: phaseSummary,
				},
			},
		},
		DominantPhaseP95: map[string]string{startClass: dominantPhase},
		DominantPhaseP99: map[string]string{startClass: dominantPhase},
		PhaseBreakdown: map[string]map[string]StartupPhaseBreakdown{
			startClass: {
				dominantPhase: {
					Count:      count,
					P95Seconds: p95,
					P99Seconds: p95 * 1.1,
				},
			},
		},
		Bundle: &BundleTemplateSummary{
			HitCount:  hitCount,
			MissCount: missCount,
		},
		Envelope: &ExecutionEnvelopeSummary{
			PreparedCount:              hitCount + missCount,
			HitCount:                   hitCount,
			MissCount:                  missCount,
			AveragePrepareDurationSec:  p95 / 4,
			AverageActivateDurationSec: p95 / 8,
		},
	}
	total := hitCount + missCount
	if total > 0 {
		summary.Bundle.HitRate = float64(hitCount) / float64(total)
	}
	return summary
}
