package natbench

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

type StartupScenarioSampleReport struct {
	Scenario    string           `json:"scenario"`
	Runtime     string           `json:"runtime"`
	RootfsType  string           `json:"rootfsType"`
	MountType   string           `json:"mountType"`
	RootfsKey   string           `json:"rootfsKey,omitempty"`
	Mode        string           `json:"mode"`
	Samples     int              `json:"samples"`
	Startup     *StartupSummary  `json:"startup,omitempty"`
	Locality    *LocalitySummary `json:"locality,omitempty"`
	StartedAt   time.Time        `json:"startedAt"`
	CompletedAt time.Time        `json:"completedAt"`
}

type StartupScenarioReport struct {
	Scenario    string           `json:"scenario"`
	Runtime     string           `json:"runtime"`
	RootfsType  string           `json:"rootfsType"`
	MountType   string           `json:"mountType"`
	RootfsKey   string           `json:"rootfsKey,omitempty"`
	ColdSamples int              `json:"coldSamples"`
	WarmSamples int              `json:"warmSamples"`
	Startup     *StartupSummary  `json:"startup,omitempty"`
	Locality    *LocalitySummary `json:"locality,omitempty"`
	StartedAt   time.Time        `json:"startedAt"`
	CompletedAt time.Time        `json:"completedAt"`
}

type StartupMatrixGateSummary struct {
	ResourceAllocateStillDominant         bool `json:"resourceAllocateStillDominant"`
	RuntimeBundlePrepareStillDominant     bool `json:"runtimeBundlePrepareStillDominant"`
	RuncEnvelopeParityAchieved            bool `json:"runcEnvelopeParityAchieved"`
	RuntimeLaunchStillDominantAfterParity bool `json:"runtimeLaunchStillDominantAfterEnvelopeParity"`
	ImagePathDominant                     bool `json:"imagePathDominant"`
	HeavierLaunchMechanismCandidate       bool `json:"heavierLaunchMechanismCandidate"`
	SnapshotRestoreStillDeferred          bool `json:"snapshotRestoreStillDeferred"`
}

type StartupMatrixReport struct {
	GeneratedAt time.Time                `json:"generatedAt"`
	Scenarios   []StartupScenarioReport  `json:"scenarios"`
	GateSummary StartupMatrixGateSummary `json:"gateSummary"`
}

func WriteStartupScenarioSampleReport(path string, report StartupScenarioSampleReport) error {
	return writeJSONReport(path, report)
}

func ReadStartupScenarioSampleReport(path string) (StartupScenarioSampleReport, error) {
	var report StartupScenarioSampleReport
	if err := readJSONReport(path, &report); err != nil {
		return report, err
	}
	return report, nil
}

func WriteStartupScenarioReport(path string, report StartupScenarioReport) error {
	return writeJSONReport(path, report)
}

func ReadStartupScenarioReport(path string) (StartupScenarioReport, error) {
	var report StartupScenarioReport
	if err := readJSONReport(path, &report); err != nil {
		return report, err
	}
	return report, nil
}

func WriteStartupMatrixReport(path string, report StartupMatrixReport) error {
	return writeJSONReport(path, report)
}

func ReadStartupMatrixReport(path string) (StartupMatrixReport, error) {
	var report StartupMatrixReport
	if err := readJSONReport(path, &report); err != nil {
		return report, err
	}
	return report, nil
}

func writeJSONReport(path string, payload any) error {
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json report: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func readJSONReport(path string, payload any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, payload); err != nil {
		return fmt.Errorf("unmarshal json report: %w", err)
	}
	return nil
}

func BuildStartupScenarioReport(samples []StartupScenarioSampleReport) (StartupScenarioReport, error) {
	if len(samples) == 0 {
		return StartupScenarioReport{}, fmt.Errorf("no startup scenario samples provided")
	}
	sorted := slices.Clone(samples)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Mode != sorted[j].Mode {
			return sorted[i].Mode < sorted[j].Mode
		}
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})

	base := sorted[0]
	report := StartupScenarioReport{
		Scenario:    base.Scenario,
		Runtime:     base.Runtime,
		RootfsType:  base.RootfsType,
		MountType:   base.MountType,
		RootfsKey:   base.RootfsKey,
		StartedAt:   base.StartedAt,
		CompletedAt: base.CompletedAt,
	}

	startupSummaries := make([]*StartupSummary, 0, len(sorted))
	for _, sample := range sorted {
		if err := validateStartupScenarioSample(base, sample); err != nil {
			return StartupScenarioReport{}, err
		}
		startupSummaries = append(startupSummaries, sample.Startup)
		switch sample.Mode {
		case "cold":
			report.ColdSamples += sample.Samples
		case "warm":
			report.WarmSamples += sample.Samples
			if sample.Locality != nil {
				report.Locality = cloneLocalitySummary(sample.Locality)
			}
		}
		if report.Locality == nil && sample.Locality != nil {
			report.Locality = cloneLocalitySummary(sample.Locality)
		}
		if sample.StartedAt.Before(report.StartedAt) {
			report.StartedAt = sample.StartedAt
		}
		if sample.CompletedAt.After(report.CompletedAt) {
			report.CompletedAt = sample.CompletedAt
		}
	}
	report.Startup = aggregateStartupSummaries(startupSummaries)
	return report, nil
}

func BuildStartupMatrixReport(scenarios []StartupScenarioReport) StartupMatrixReport {
	sorted := slices.Clone(scenarios)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Scenario < sorted[j].Scenario
	})
	report := StartupMatrixReport{
		GeneratedAt: time.Now().UTC(),
		Scenarios:   sorted,
	}

	warmScenarioCount := 0
	runtimeLaunchDominantCount := 0
	for _, scenario := range sorted {
		if scenario.Startup == nil {
			continue
		}
		dominantP95 := strings.TrimSpace(scenario.Startup.DominantPhaseP95["warm"])
		dominantP99 := strings.TrimSpace(scenario.Startup.DominantPhaseP99["warm"])
		if dominantP95 == "" && dominantP99 == "" {
			continue
		}
		warmScenarioCount++
		if scenario.Runtime == "runc" && scenario.Startup.Envelope != nil &&
			scenario.Startup.Envelope.PreparedCount > 0 &&
			scenario.Startup.Envelope.HitCount > 0 {
			report.GateSummary.RuncEnvelopeParityAchieved = true
		}
		if dominantP95 == "resource_allocate" || dominantP99 == "resource_allocate" {
			report.GateSummary.ResourceAllocateStillDominant = true
		}
		if dominantP95 == "runtime_bundle_prepare" || dominantP99 == "runtime_bundle_prepare" {
			report.GateSummary.RuntimeBundlePrepareStillDominant = true
		}
		if dominantP95 == "runtime_launch" || dominantP99 == "runtime_launch" {
			runtimeLaunchDominantCount++
		}
		if scenario.RootfsType != "local" && (dominantP95 == "rootfs_prepare" || dominantP99 == "rootfs_prepare") {
			report.GateSummary.ImagePathDominant = true
		}
	}

	if warmScenarioCount > 0 && runtimeLaunchDominantCount*2 >= warmScenarioCount {
		report.GateSummary.RuntimeLaunchStillDominantAfterParity = true
	}
	report.GateSummary.HeavierLaunchMechanismCandidate =
		report.GateSummary.RuncEnvelopeParityAchieved &&
			!report.GateSummary.ResourceAllocateStillDominant &&
			!report.GateSummary.RuntimeBundlePrepareStillDominant &&
			report.GateSummary.RuntimeLaunchStillDominantAfterParity
	report.GateSummary.SnapshotRestoreStillDeferred = true
	return report
}

func validateStartupScenarioSample(base, sample StartupScenarioSampleReport) error {
	switch {
	case base.Scenario != sample.Scenario:
		return fmt.Errorf("scenario mismatch: %q != %q", sample.Scenario, base.Scenario)
	case base.Runtime != sample.Runtime:
		return fmt.Errorf("runtime mismatch: %q != %q", sample.Runtime, base.Runtime)
	case base.RootfsType != sample.RootfsType:
		return fmt.Errorf("rootfsType mismatch: %q != %q", sample.RootfsType, base.RootfsType)
	case base.MountType != sample.MountType:
		return fmt.Errorf("mountType mismatch: %q != %q", sample.MountType, base.MountType)
	case base.RootfsKey != sample.RootfsKey:
		return fmt.Errorf("rootfsKey mismatch: %q != %q", sample.RootfsKey, base.RootfsKey)
	default:
		return nil
	}
}

func cloneLocalitySummary(in *LocalitySummary) *LocalitySummary {
	if in == nil {
		return nil
	}
	out := &LocalitySummary{
		Key: in.Key,
	}
	if in.Entry != nil {
		entry := *in.Entry
		out.Entry = &entry
	}
	out.Ranked = slices.Clone(in.Ranked)
	return out
}
