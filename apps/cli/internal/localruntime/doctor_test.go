package localruntime

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadMaterializedDNSNameservers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compose.env")
	if err := os.WriteFile(path, []byte("OTHER=value\nAXNODED_DNS_NAMESERVERS=\"192.0.2.53,2001:db8::53,192.0.2.53\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readMaterializedDNSNameservers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "192.0.2.53" || got[1] != "2001:db8::53" {
		t.Fatalf("nameservers = %v", got)
	}
}

func TestReadMaterializedDNSNameserversAllowsNodeDerivedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compose.env")
	if err := os.WriteFile(path, []byte("AXNODED_DNS_NAMESERVERS=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readMaterializedDNSNameservers(path)
	if err != nil || len(got) != 0 {
		t.Fatalf("node-derived materialized DNS = (%v, %v), want empty valid override", got, err)
	}
}

func TestDoctorDNSNameserversPrefersMaterializedConfiguration(t *testing.T) {
	t.Setenv(localDNSNameserversEnv, "198.51.100.53")
	dir := t.TempDir()
	if err := saveMetadata(filepath.Join(dir, "metadata.json"), Metadata{Version: "dev", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.env"), []byte("AXNODED_DNS_NAMESERVERS=192.0.2.53\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (&Manager{Dir: dir}).doctorDNSNameservers(doctorDNSConfigApplied)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "192.0.2.53" {
		t.Fatalf("nameservers = %v, want materialized resolver", got)
	}
}

func TestDoctorDNSNameserversDesiredIgnoresBrokenMaterializedConfiguration(t *testing.T) {
	t.Setenv(localDNSNameserversEnv, "198.51.100.53")
	dir := t.TempDir()
	if err := saveMetadata(filepath.Join(dir, "metadata.json"), Metadata{Version: "dev", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.env"), []byte("AXNODED_DNS_NAMESERVERS=not-an-address\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := (&Manager{Dir: dir}).doctorDNSNameservers(doctorDNSConfigDesired)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "198.51.100.53" {
		t.Fatalf("nameservers = %v, want desired host resolver", got)
	}
}

func TestDoctorNodeIDUsesMaterializedConfiguration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.env"), []byte("AXNODED_CONTROL_PLANE_NODE_ID=node-compose-local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := (&Manager{Dir: dir}).doctorNodeID(); got != "node-compose-local" {
		t.Fatalf("doctorNodeID() = %q", got)
	}
}

func TestDoctorReportFolding(t *testing.T) {
	report := newDoctorReport(false)
	report.add(Check{Status: checkSkip})
	report.add(Check{Status: checkWarn})
	if report.Status != doctorDegraded {
		t.Fatalf("status = %q, want degraded", report.Status)
	}
	report.add(Check{Status: checkFail})
	report.add(Check{Status: checkWarn})
	if report.Status != doctorFailed {
		t.Fatalf("status = %q, want failed", report.Status)
	}
}

func TestProbeNodeDNSMapsSanitizedPartialResult(t *testing.T) {
	runner := staticOutputRunner{data: []byte(`{"status":"warn","code":"runtime_dns_node_partial","effective_resolver_count":2,"successful_resolver_count":1}`)}
	manager := &Manager{Dir: t.TempDir(), Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	check := manager.probeNodeDNS(context.Background(), "", "private.corp.example.", time.Second)
	if check.Status != checkWarn || check.Code != "runtime_dns_node_partial" || check.Details["effective_resolver_count"] != 2 {
		t.Fatalf("check = %#v", check)
	}
	if _, ok := check.Details["query_name"]; ok {
		t.Fatalf("details leaked query metadata: %#v", check.Details)
	}
}

func TestProbeNodeDNSRejectsInconsistentResult(t *testing.T) {
	runner := staticOutputRunner{data: []byte(`{"status":"pass","code":"runtime_dns_node_partial","effective_resolver_count":2,"successful_resolver_count":1}`)}
	manager := &Manager{Dir: t.TempDir(), Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	check := manager.probeNodeDNS(context.Background(), "", "example.test.", time.Second)
	if check.Status != checkFail || check.Code != "runtime_dns_node_unreachable" || check.Details != nil {
		t.Fatalf("check = %#v", check)
	}
}

type staticOutputRunner struct{ data []byte }

func (r staticOutputRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return r.data, nil
}
func (staticOutputRunner) Run(context.Context, io.Writer, io.Writer, string, ...string) error {
	return nil
}
