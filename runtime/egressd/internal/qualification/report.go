package qualification

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 2

var (
	Runtimes        = []string{"runc", "runsc"}
	NetworkBackends = []string{"bridge", "ebpf"}
	IPFamilies      = []string{"ipv4", "ipv6"}
	PolicyModes     = []string{"unrestricted", "dns_deny", "strict_domain", "strict_cidr"}
)

type EnvironmentProvenance struct {
	EnvironmentID        string            `json:"environmentId"`
	OS                   string            `json:"os"`
	Architecture         string            `json:"architecture"`
	KernelRelease        string            `json:"kernelRelease"`
	CPUModel             string            `json:"cpuModel"`
	LogicalCPUs          int               `json:"logicalCpus"`
	MemoryBytes          uint64            `json:"memoryBytes"`
	HostIdentityDigest   string            `json:"hostIdentityDigest"`
	SystemPackagesDigest string            `json:"systemPackagesDigest"`
	RuntimeDigests       map[string]string `json:"runtimeDigests"`
}

type SubjectProvenance struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
	Build  string `json:"build"`
}

type Parameters struct {
	Samples          int      `json:"samples"`
	Concurrency      int      `json:"concurrency"`
	PayloadBytes     int      `json:"payloadBytes"`
	SustainedSeconds int      `json:"sustainedSeconds"`
	RuleScaleCounts  []uint32 `json:"ruleScaleCounts"`
}

type Distribution struct {
	Samples int     `json:"samples"`
	P50     float64 `json:"p50"`
	P95     float64 `json:"p95"`
	P99     float64 `json:"p99"`
	Max     float64 `json:"max"`
}

type RuleScalePoint struct {
	Rules          uint32  `json:"rules"`
	PrepareP95MS   float64 `json:"prepareP95Milliseconds"`
	ReconcileP95MS float64 `json:"reconcileP95Milliseconds"`
	RSSBytes       uint64  `json:"rssBytes"`
}

type ScenarioMetrics struct {
	PrepareLatencyMS            *Distribution    `json:"prepareLatencyMilliseconds,omitempty"`
	SandboxStartLatencyMS       *Distribution    `json:"sandboxStartLatencyMilliseconds,omitempty"`
	DNSLatencyMS                *Distribution    `json:"dnsLatencyMilliseconds,omitempty"`
	FirstConnectionLatencyMS    *Distribution    `json:"firstConnectionLatencyMilliseconds,omitempty"`
	RestartConvergenceLatencyMS *Distribution    `json:"restartConvergenceLatencyMilliseconds,omitempty"`
	HTTPThroughputMbps          *float64         `json:"httpThroughputMbps,omitempty"`
	TLSThroughputMbps           *float64         `json:"tlsThroughputMbps,omitempty"`
	MaxRSSBytes                 uint64           `json:"maxRssBytes"`
	PeakConcurrentSessions      uint32           `json:"peakConcurrentSessions"`
	Operations                  uint64           `json:"operations"`
	Failures                    uint64           `json:"failures"`
	RuleScale                   []RuleScalePoint `json:"ruleScale,omitempty"`
}

type ScenarioResult struct {
	Runtime        string          `json:"runtime"`
	NetworkBackend string          `json:"networkBackend"`
	IPFamily       string          `json:"ipFamily"`
	PolicyMode     string          `json:"policyMode"`
	Metrics        ScenarioMetrics `json:"metrics"`
}

type Report struct {
	SchemaVersion int                   `json:"schemaVersion"`
	GeneratedAt   time.Time             `json:"generatedAt"`
	Environment   EnvironmentProvenance `json:"environment"`
	Subject       SubjectProvenance     `json:"subject"`
	Parameters    Parameters            `json:"parameters"`
	Scenarios     []ScenarioResult      `json:"scenarios"`
}

func DecodeReport(reader io.Reader) (Report, error) {
	var report Report
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return Report{}, fmt.Errorf("decode qualification report: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Report{}, err
	}
	return report, nil
}

func DecodeScenario(reader io.Reader) (ScenarioResult, error) {
	var scenario ScenarioResult
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&scenario); err != nil {
		return ScenarioResult{}, fmt.Errorf("decode qualification scenario: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return ScenarioResult{}, err
	}
	return scenario, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("qualification JSON contains multiple values")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing qualification JSON: %w", err)
	}
	return nil
}

func ReadReport(path string) (Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer file.Close()
	return DecodeReport(file)
}

func WriteReport(writer io.Writer, report Report) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func (report *Report) Normalize() {
	sort.Slice(report.Scenarios, func(i, j int) bool {
		return scenarioKey(report.Scenarios[i]) < scenarioKey(report.Scenarios[j])
	})
	for index := range report.Scenarios {
		sort.Slice(report.Scenarios[index].Metrics.RuleScale, func(i, j int) bool {
			return report.Scenarios[index].Metrics.RuleScale[i].Rules < report.Scenarios[index].Metrics.RuleScale[j].Rules
		})
	}
	sort.Slice(report.Parameters.RuleScaleCounts, func(i, j int) bool {
		return report.Parameters.RuleScaleCounts[i] < report.Parameters.RuleScaleCounts[j]
	})
}

func (report Report) Validate(fullMatrix bool) error {
	if report.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", report.SchemaVersion, SchemaVersion)
	}
	if report.GeneratedAt.IsZero() {
		return errors.New("generatedAt is required")
	}
	if !validCommit(report.Subject.Commit) || !validDigest(report.Subject.Build) || report.Subject.Dirty {
		return errors.New("clean subject commit and build identity are required")
	}
	if err := report.Environment.Validate(); err != nil {
		return err
	}
	if report.Parameters.Samples <= 0 || report.Parameters.Concurrency <= 0 || report.Parameters.PayloadBytes <= 0 || report.Parameters.SustainedSeconds <= 0 {
		return errors.New("positive samples, concurrency, payloadBytes, and sustainedSeconds are required")
	}
	wantedRuleCounts := map[uint32]struct{}{}
	for _, count := range report.Parameters.RuleScaleCounts {
		if count == 0 {
			return errors.New("ruleScaleCounts must contain only positive values")
		}
		if _, exists := wantedRuleCounts[count]; exists {
			return fmt.Errorf("duplicate ruleScaleCount %d", count)
		}
		wantedRuleCounts[count] = struct{}{}
	}
	if len(wantedRuleCounts) == 0 {
		return errors.New("at least one ruleScaleCount is required")
	}
	if len(report.Scenarios) == 0 {
		return errors.New("at least one qualification scenario is required")
	}
	seen := map[string]struct{}{}
	for index, scenario := range report.Scenarios {
		if err := scenario.Validate(); err != nil {
			return fmt.Errorf("scenario %d: %w", index, err)
		}
		for name, distribution := range map[string]*Distribution{
			"prepare": scenario.Metrics.PrepareLatencyMS, "sandbox-start": scenario.Metrics.SandboxStartLatencyMS,
			"dns": scenario.Metrics.DNSLatencyMS, "first-connection": scenario.Metrics.FirstConnectionLatencyMS,
			"restart-convergence": scenario.Metrics.RestartConvergenceLatencyMS,
		} {
			if distribution != nil && distribution.Samples != report.Parameters.Samples {
				return fmt.Errorf("scenario %d %s samples = %d, want %d", index, name, distribution.Samples, report.Parameters.Samples)
			}
		}
		if len(scenario.Metrics.RuleScale) != len(wantedRuleCounts) {
			return fmt.Errorf("scenario %d rule-scale point count = %d, want %d", index, len(scenario.Metrics.RuleScale), len(wantedRuleCounts))
		}
		for _, point := range scenario.Metrics.RuleScale {
			if _, exists := wantedRuleCounts[point.Rules]; !exists {
				return fmt.Errorf("scenario %d has unexpected rule-scale count %d", index, point.Rules)
			}
		}
		key := scenarioKey(scenario)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate scenario %q", key)
		}
		seen[key] = struct{}{}
	}
	if fullMatrix {
		for _, runtimeName := range Runtimes {
			for _, backend := range NetworkBackends {
				for _, family := range IPFamilies {
					for _, mode := range PolicyModes {
						key := strings.Join([]string{runtimeName, backend, family, mode}, "/")
						if _, exists := seen[key]; !exists {
							return fmt.Errorf("full matrix is missing scenario %q", key)
						}
					}
				}
			}
		}
		if len(seen) != len(Runtimes)*len(NetworkBackends)*len(IPFamilies)*len(PolicyModes) {
			return fmt.Errorf("full matrix contains %d scenarios, want %d", len(seen), len(Runtimes)*len(NetworkBackends)*len(IPFamilies)*len(PolicyModes))
		}
	}
	return nil
}

func (environment EnvironmentProvenance) Validate() error {
	if environment.OS != "linux" || environment.Architecture == "" || environment.KernelRelease == "" || environment.CPUModel == "" || environment.LogicalCPUs <= 0 || environment.MemoryBytes == 0 {
		return errors.New("complete Linux environment provenance is required")
	}
	if !validDigest(environment.HostIdentityDigest) || !validDigest(environment.SystemPackagesDigest) {
		return errors.New("hostIdentityDigest and systemPackagesDigest must be sha256 digests")
	}
	for _, runtimeName := range Runtimes {
		if !validDigest(environment.RuntimeDigests[runtimeName]) {
			return fmt.Errorf("runtimeDigests[%q] must be a sha256 digest", runtimeName)
		}
	}
	expected, err := environment.Fingerprint()
	if err != nil {
		return err
	}
	if environment.EnvironmentID != expected {
		return fmt.Errorf("environmentId does not match canonical provenance")
	}
	return nil
}

func (environment EnvironmentProvenance) Fingerprint() (string, error) {
	environment.EnvironmentID = ""
	wire, err := json.Marshal(environment)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (scenario ScenarioResult) Validate() error {
	if !contains(Runtimes, scenario.Runtime) || !contains(NetworkBackends, scenario.NetworkBackend) || !contains(IPFamilies, scenario.IPFamily) || !contains(PolicyModes, scenario.PolicyMode) {
		return fmt.Errorf("invalid scenario axes %q", scenarioKey(scenario))
	}
	metrics := scenario.Metrics
	if metrics.PrepareLatencyMS == nil || metrics.SandboxStartLatencyMS == nil || metrics.FirstConnectionLatencyMS == nil || metrics.RestartConvergenceLatencyMS == nil {
		return errors.New("prepare, sandbox-start, first-connection, and restart-convergence distributions are required")
	}
	for name, distribution := range map[string]*Distribution{
		"prepare": metrics.PrepareLatencyMS, "sandbox-start": metrics.SandboxStartLatencyMS,
		"dns": metrics.DNSLatencyMS, "first-connection": metrics.FirstConnectionLatencyMS,
		"restart-convergence": metrics.RestartConvergenceLatencyMS,
	} {
		if distribution != nil {
			if err := distribution.Validate(); err != nil {
				return fmt.Errorf("%s latency: %w", name, err)
			}
		}
	}
	for name, throughput := range map[string]*float64{"http": metrics.HTTPThroughputMbps, "tls": metrics.TLSThroughputMbps} {
		if throughput != nil && (!finite(*throughput) || *throughput <= 0) {
			return fmt.Errorf("%s throughput must be finite and positive", name)
		}
	}
	if metrics.MaxRSSBytes == 0 || metrics.PeakConcurrentSessions == 0 || metrics.Operations == 0 || metrics.Failures > metrics.Operations {
		return errors.New("positive RSS, concurrency, operations, and bounded failures are required")
	}
	seenRules := map[uint32]struct{}{}
	for _, point := range metrics.RuleScale {
		if point.Rules == 0 || point.RSSBytes == 0 || !finite(point.PrepareP95MS) || point.PrepareP95MS < 0 || !finite(point.ReconcileP95MS) || point.ReconcileP95MS < 0 {
			return errors.New("rule-scale points require positive rules/RSS and finite non-negative latency")
		}
		if _, exists := seenRules[point.Rules]; exists {
			return fmt.Errorf("duplicate rule-scale count %d", point.Rules)
		}
		seenRules[point.Rules] = struct{}{}
	}
	return nil
}

func (distribution Distribution) Validate() error {
	if distribution.Samples <= 0 || !finite(distribution.P50) || !finite(distribution.P95) || !finite(distribution.P99) || !finite(distribution.Max) || distribution.P50 < 0 || distribution.P50 > distribution.P95 || distribution.P95 > distribution.P99 || distribution.P99 > distribution.Max {
		return errors.New("distribution requires positive samples and ordered finite non-negative quantiles")
	}
	return nil
}

func scenarioKey(scenario ScenarioResult) string {
	return strings.Join([]string{scenario.Runtime, scenario.NetworkBackend, scenario.IPFamily, scenario.PolicyMode}, "/")
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validCommit(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func CanonicalParameters(parameters Parameters) ([]byte, error) {
	copy := parameters
	copy.RuleScaleCounts = append([]uint32(nil), parameters.RuleScaleCounts...)
	sort.Slice(copy.RuleScaleCounts, func(i, j int) bool { return copy.RuleScaleCounts[i] < copy.RuleScaleCounts[j] })
	return json.Marshal(copy)
}

func EqualParameters(left, right Parameters) bool {
	leftWire, leftErr := CanonicalParameters(left)
	rightWire, rightErr := CanonicalParameters(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftWire, rightWire)
}
