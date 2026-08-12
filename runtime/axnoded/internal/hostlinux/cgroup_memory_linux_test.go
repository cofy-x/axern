//go:build linux

package hostlinux

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestConfigureCgroupMemoryControlsGroupsParentAndLeafOOM(t *testing.T) {
	parent := t.TempDir()
	leaf := filepath.Join(parent, "workload")
	if err := os.Mkdir(leaf, 0o755); err != nil {
		t.Fatalf("create workload cgroup fixture: %v", err)
	}
	for _, dir := range []string{parent, leaf} {
		writeFixtureFile(t, dir, "memory.max", "0\n")
		writeFixtureFile(t, dir, "memory.swap.max", "9\n")
		writeFixtureFile(t, dir, "memory.oom.group", "0\n")
	}
	const limit = int64(64 << 20)
	if err := configureCgroupMemoryControls(parent, leaf, limit); err != nil {
		t.Fatalf("configureCgroupMemoryControls() error = %v", err)
	}
	for _, dir := range []string{parent, leaf} {
		for name, want := range map[string]string{
			"memory.max": strconv.FormatInt(limit, 10), "memory.swap.max": "0", "memory.oom.group": "1",
		} {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if got := strings.TrimSpace(string(data)); got != want {
				t.Fatalf("%s in %s = %q, want %q", name, dir, got, want)
			}
		}
	}
}

func TestReadCgroupMemoryObservationAllowsUnavailablePSI(t *testing.T) {
	dir := writeMemoryObservationFixture(t)

	observation, err := readCgroupMemoryObservationDir(dir)
	if err != nil {
		t.Fatalf("readCgroupMemoryObservationDir() error = %v", err)
	}
	if observation.PSIAvailable {
		t.Fatal("PSIAvailable = true without memory.pressure")
	}
	if observation.CurrentBytes != 10 || observation.PeakBytes != 20 || !observation.PeakAvailable || observation.Events["oom_kill"] != 1 {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestReadCgroupMemoryObservationUsesCurrentWhenKernelPeakIsUnavailable(t *testing.T) {
	dir := writeMemoryObservationFixture(t)
	if err := os.Remove(filepath.Join(dir, "memory.peak")); err != nil {
		t.Fatalf("remove memory.peak: %v", err)
	}

	observation, err := readCgroupMemoryObservationDir(dir)
	if err != nil {
		t.Fatalf("readCgroupMemoryObservationDir() error = %v", err)
	}
	if observation.PeakAvailable || observation.PeakBytes != observation.CurrentBytes {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestOptionalCgroupMemoryInterfaceUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "missing", err: fmt.Errorf("read optional file: %w", os.ErrNotExist), want: true},
		{name: "unsupported", err: &os.PathError{Op: "open", Path: "memory.pressure", Err: syscall.EOPNOTSUPP}, want: true},
		{name: "permission denied", err: &os.PathError{Op: "open", Path: "memory.pressure", Err: syscall.EACCES}, want: false},
		{name: "malformed", err: fmt.Errorf("malformed PSI field"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := optionalCgroupMemoryInterfaceUnavailable(tt.err); got != tt.want {
				t.Fatalf("optionalCgroupMemoryInterfaceUnavailable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestReclaimCgroupMemoryDirTreatsMissingInterfaceAsOptional(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "memory.current", "4096\n")

	result, err := reclaimCgroupMemoryDir(dir)
	if err != nil {
		t.Fatalf("reclaimCgroupMemoryDir() error = %v", err)
	}
	if result != CgroupMemoryReclaimUnavailable {
		t.Fatalf("reclaimCgroupMemoryDir() = %d, want unavailable", result)
	}
}

func TestReclaimCgroupMemoryDirRequestsCurrentCharge(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "memory.current", "4096\n")
	if err := os.WriteFile(filepath.Join(dir, "memory.reclaim"), nil, 0o600); err != nil {
		t.Fatalf("create memory.reclaim fixture: %v", err)
	}

	result, err := reclaimCgroupMemoryDir(dir)
	if err != nil {
		t.Fatalf("reclaimCgroupMemoryDir() error = %v", err)
	}
	if result != CgroupMemoryReclaimRequested {
		t.Fatalf("reclaimCgroupMemoryDir() = %d, want requested", result)
	}
	payload, err := os.ReadFile(filepath.Join(dir, "memory.reclaim"))
	if err != nil {
		t.Fatalf("read memory.reclaim fixture: %v", err)
	}
	if got := strings.TrimSpace(string(payload)); got != "4096" {
		t.Fatalf("memory.reclaim = %q, want 4096", got)
	}
}

func TestReclaimCgroupMemoryDirSkipsZeroCharge(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "memory.current", "0\n")

	result, err := reclaimCgroupMemoryDir(dir)
	if err != nil {
		t.Fatalf("reclaimCgroupMemoryDir() error = %v", err)
	}
	if result != CgroupMemoryReclaimNotNeeded {
		t.Fatalf("reclaimCgroupMemoryDir() = %d, want not needed", result)
	}
}

func TestClassifyCgroupMemoryReclaimWrite(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantResult CgroupMemoryReclaimResult
		wantError  bool
	}{
		{name: "accepted", wantResult: CgroupMemoryReclaimRequested},
		{name: "partial", err: syscall.EAGAIN, wantResult: CgroupMemoryReclaimRequested},
		{name: "missing", err: os.ErrNotExist, wantResult: CgroupMemoryReclaimUnavailable},
		{name: "unsupported", err: syscall.EOPNOTSUPP, wantResult: CgroupMemoryReclaimUnavailable},
		{name: "permission", err: os.ErrPermission, wantResult: CgroupMemoryReclaimNotNeeded, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := classifyCgroupMemoryReclaimWrite(tt.err)
			if result != tt.wantResult || (err != nil) != tt.wantError {
				t.Fatalf("classifyCgroupMemoryReclaimWrite(%v) = (%d, %v), want (%d, error=%t)", tt.err, result, err, tt.wantResult, tt.wantError)
			}
		})
	}
}

func TestReadCgroupMemoryObservationRejectsKernelPeakBelowCurrent(t *testing.T) {
	dir := writeMemoryObservationFixture(t)
	writeFixtureFile(t, dir, "memory.peak", "9\n")

	if _, err := readCgroupMemoryObservationDir(dir); err == nil || !strings.Contains(err.Error(), "below memory.current") {
		t.Fatalf("readCgroupMemoryObservationDir() error = %v", err)
	}
}

func TestReadCgroupMemoryObservationReportsAvailablePSI(t *testing.T) {
	dir := writeMemoryObservationFixture(t)
	writeFixtureFile(t, dir, "memory.pressure", "some avg10=1.25 avg60=0.50 avg300=0.10 total=41\nfull avg10=0.25 avg60=0.10 avg300=0.01 total=7\n")

	observation, err := readCgroupMemoryObservationDir(dir)
	if err != nil {
		t.Fatalf("readCgroupMemoryObservationDir() error = %v", err)
	}
	if !observation.PSIAvailable || observation.PSISomeAvg10 != 1.25 || observation.PSIFullAvg10 != 0.25 || observation.PSISomeTotal != 41 || observation.PSIFullTotal != 7 {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestReadCgroupMemoryObservationRejectsMalformedPSI(t *testing.T) {
	dir := writeMemoryObservationFixture(t)
	writeFixtureFile(t, dir, "memory.pressure", "some malformed\n")

	_, err := readCgroupMemoryObservationDir(dir)
	if err == nil || !strings.Contains(err.Error(), "malformed PSI field") {
		t.Fatalf("readCgroupMemoryObservationDir() error = %v, want malformed PSI", err)
	}
}

func TestReadCgroupMemoryObservationRejectsIncompletePSI(t *testing.T) {
	dir := writeMemoryObservationFixture(t)
	writeFixtureFile(t, dir, "memory.pressure", "some avg10=1.25 avg60=0.50 avg300=0.10 total=41\n")

	_, err := readCgroupMemoryObservationDir(dir)
	if err == nil || !strings.Contains(err.Error(), "incomplete PSI value") {
		t.Fatalf("readCgroupMemoryObservationDir() error = %v, want incomplete PSI", err)
	}
}

func TestReadCgroupMemoryObservationRejectsDuplicatePSI(t *testing.T) {
	dir := writeMemoryObservationFixture(t)
	writeFixtureFile(t, dir, "memory.pressure", "some avg10=1.25 total=41\nsome avg10=0.25 total=7\nfull avg10=0.25 total=7\n")

	_, err := readCgroupMemoryObservationDir(dir)
	if err == nil || !strings.Contains(err.Error(), "duplicate PSI some record") {
		t.Fatalf("readCgroupMemoryObservationDir() error = %v, want duplicate PSI", err)
	}
}

func writeMemoryObservationFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixtureFile(t, dir, "memory.current", "10\n")
	writeFixtureFile(t, dir, "memory.peak", "20\n")
	writeFixtureFile(t, dir, "memory.swap.current", "0\n")
	writeFixtureFile(t, dir, "memory.stat", "anon 4\nfile 6\n")
	writeFixtureFile(t, dir, "memory.events", "high 0\nmax 1\noom 1\noom_kill 1\n")
	return dir
}

func writeFixtureFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
