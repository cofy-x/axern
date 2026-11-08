//go:build linux || darwin

package localruntime

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDirectoryAllocatedSizeSkipsUnreadableRuntimeState(t *testing.T) {
	const childState = "AXERN_DISK_USAGE_TEST_CHILD"
	if dir := os.Getenv(childState); dir != "" {
		if os.Geteuid() == 0 {
			t.Fatal("permission fixture must run without root privileges")
		}
		assertUnreadableRuntimeState(t, dir)
		return
	}

	// Root can traverse chmod(000) directories. Keep the fixture genuinely
	// inaccessible to a separate Unix identity rather than dropping capabilities
	// from unrelated filesystem and runtime verification in the parent process.
	dir, err := os.MkdirTemp("", "axern-disk-permission-")
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	private := filepath.Join(state, "identity")
	t.Cleanup(func() {
		_ = os.Chmod(private, 0o700)
		_ = os.RemoveAll(dir)
	})
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "node.pem"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if err := os.Chmod(private, 0); err != nil {
			t.Fatal(err)
		}
		assertUnreadableRuntimeState(t, state)
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe.test")
	if err := os.WriteFile(probe, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(probe, "-test.run=^TestDirectoryAllocatedSizeSkipsUnreadableRuntimeState$")
	command.Dir = dir
	command.Env = []string{childState + "=" + state}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("unprivileged disk diagnostic: %v\n%s", err, output)
	}
}

func assertUnreadableRuntimeState(t *testing.T, dir string) {
	t.Helper()
	_, partial, err := directoryAllocatedSize(dir)
	if err != nil {
		t.Fatalf("root-owned runtime state made disk diagnostics fail: %v", err)
	}
	if !partial {
		t.Fatal("unreadable runtime state was not reported as partial")
	}
}
