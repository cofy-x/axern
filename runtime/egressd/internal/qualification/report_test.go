package qualification

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testCommit = "0123456789abcdef0123456789abcdef01234567"
	testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestReportValidatesCompleteMatrixAndCanonicalEnvironment(t *testing.T) {
	report := testReport(t)
	if err := report.Validate(true); err != nil {
		t.Fatal(err)
	}
	report.Scenarios = report.Scenarios[1:]
	if err := report.Validate(true); err == nil || !strings.Contains(err.Error(), "missing scenario") {
		t.Fatalf("incomplete matrix validation = %v", err)
	}
}

func TestDecodeReportRejectsPrivacySensitiveAndUnknownFields(t *testing.T) {
	report := testReport(t)
	var output bytes.Buffer
	if err := WriteReport(&output, report); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(output.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	raw["destinationHost"] = "private.destination.example"
	wire, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReport(bytes.NewReader(wire)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("privacy-sensitive unknown field validation = %v", err)
	}
	for _, forbidden := range []string{"destination", "domainName", "hostName", "sni", "remoteIp", "cidrValue", "policyDigest", "rawPolicy"} {
		if bytes.Contains(bytes.ToLower(output.Bytes()), bytes.ToLower([]byte(forbidden))) {
			t.Fatalf("privacy-sensitive key %q entered report: %s", forbidden, output.String())
		}
	}
}

func TestReadSubjectCommitRejectsUnboundOrOversizedRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subject")
	if err := os.WriteFile(path, []byte("\n"+testCommit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadSubjectCommit(path); err != nil || got != testCommit {
		t.Fatalf("ReadSubjectCommit() = %q, %v", got, err)
	}
	for _, invalid := range []string{"development", strings.ToUpper(testCommit), strings.Repeat("a", 1025)} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadSubjectCommit(path); err == nil {
			t.Fatalf("ReadSubjectCommit() accepted %q", invalid[:min(len(invalid), 32)])
		}
	}
}

func TestEnvironmentFingerprintChangesOnlyWithEnvironment(t *testing.T) {
	report := testReport(t)
	first := report.Environment.EnvironmentID
	report.Subject.Commit = strings.Repeat("b", 40)
	report.Subject.Build = "sha256:" + strings.Repeat("c", 64)
	second, err := report.Environment.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatal("subject identity changed the environment fingerprint")
	}
	report.Environment.KernelRelease = "different"
	third, err := report.Environment.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("kernel change retained the environment fingerprint")
	}
	report = testReport(t)
	report.Environment.SystemPackagesDigest = "sha256:" + strings.Repeat("d", 64)
	fourth, err := report.Environment.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fourth == first {
		t.Fatal("system package change retained the environment fingerprint")
	}
	report = testReport(t)
	report.Environment.HostIdentityDigest = "sha256:" + strings.Repeat("e", 64)
	fifth, err := report.Environment.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fifth == first {
		t.Fatal("host identity change retained the environment fingerprint")
	}
}

func testReport(t *testing.T) Report {
	t.Helper()
	environment := EnvironmentProvenance{
		OS: "linux", Architecture: "amd64", KernelRelease: "6.12.0", CPUModel: "qualification cpu",
		LogicalCPUs: 8, MemoryBytes: 16 << 30, HostIdentityDigest: testDigest, SystemPackagesDigest: testDigest,
		RuntimeDigests: map[string]string{"runc": testDigest, "runsc": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}
	var err error
	environment.EnvironmentID, err = environment.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	report := Report{
		SchemaVersion: SchemaVersion, GeneratedAt: time.Unix(1_800_000_000, 0).UTC(), Environment: environment,
		Subject:    SubjectProvenance{Commit: testCommit, Build: testDigest},
		Parameters: Parameters{Samples: 20, Concurrency: 16, PayloadBytes: 1 << 20, SustainedSeconds: 60, RuleScaleCounts: []uint32{1, 64, 256}},
	}
	for _, runtimeName := range Runtimes {
		for _, backend := range NetworkBackends {
			for _, family := range IPFamilies {
				for _, mode := range PolicyModes {
					report.Scenarios = append(report.Scenarios, testScenario(runtimeName, backend, family, mode))
				}
			}
		}
	}
	return report
}

func testScenario(runtimeName, backend, family, mode string) ScenarioResult {
	distribution := func(value float64) *Distribution {
		return &Distribution{Samples: 20, P50: value, P95: value * 1.1, P99: value * 1.2, Max: value * 1.3}
	}
	httpThroughput := 1000.0
	tlsThroughput := 900.0
	return ScenarioResult{
		Runtime: runtimeName, NetworkBackend: backend, IPFamily: family, PolicyMode: mode,
		Metrics: ScenarioMetrics{
			PrepareLatencyMS: distribution(1), SandboxStartLatencyMS: distribution(2), DNSLatencyMS: distribution(3),
			FirstConnectionLatencyMS: distribution(4), RestartConvergenceLatencyMS: distribution(5),
			HTTPThroughputMbps: &httpThroughput, TLSThroughputMbps: &tlsThroughput,
			MaxRSSBytes: 64 << 20, PeakConcurrentSessions: 256, Operations: 10_000,
			RuleScale: []RuleScalePoint{{Rules: 1, PrepareP95MS: 1, ReconcileP95MS: 2, RSSBytes: 64 << 20}, {Rules: 64, PrepareP95MS: 4, ReconcileP95MS: 5, RSSBytes: 65 << 20}, {Rules: 256, PrepareP95MS: 8, ReconcileP95MS: 9, RSSBytes: 66 << 20}},
		},
	}
}
