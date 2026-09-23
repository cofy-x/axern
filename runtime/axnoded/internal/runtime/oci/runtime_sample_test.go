package oci

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

// Compare effective policy, not just raw defaults. The Linux verification image
// supplies the repository-pinned runsc. A captured spec may also be checked on
// a host that cannot execute that binary; no sample is a production input.
func TestPlatformPolicyAgainstRunscSample(t *testing.T) {
	path := os.Getenv("AXERN_TEST_RUNSC_SPEC")
	if path == "" {
		binary, err := exec.LookPath("runsc")
		if err != nil {
			t.Skip("runsc is not installed")
		}
		dir := t.TempDir()
		cmd := exec.Command(binary, "spec")
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("runsc spec: %v: %s", err, output)
		}
		path = filepath.Join(dir, "config.json")
	}
	sample, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	sample.Process.Terminal = false // previous deployment policy
	root := t.TempDir()
	binaryPath := filepath.Join(root, "sandboxd")
	if err := os.WriteFile(binaryPath, []byte("test binary"), 0700); err != nil {
		t.Fatal(err)
	}
	loader, err := NewBundleLoader(filepath.Join(root, "bundles"))
	if err != nil {
		t.Fatal(err)
	}
	opts := LoadOptions{ContainerID: "allocation-policy", Request: &apipb.CreateContainerRequest{Command: []string{"/bin/true"}, Rootfs: &apipb.Rootfs{RootDir: root, Readonly: true}}, SandboxdInjection: &SandboxdInjectionOptions{HostBinaryPath: binaryPath}}
	loader.baseSpec = sample // test the complete previous materialization path
	_, previous, err := loader.Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	loader.baseSpec = defaultBundleSpec()
	_, current, err := loader.Generate(opts)
	if err != nil {
		t.Fatal(err)
	}
	// The deliberate semantic differences are no synthetic TERM and an explicit
	// isolated devpts mount for the existing PTY contract. All security policy
	// (capability sets, limits, namespaces, readonly flags and other mounts) must
	// remain equivalent after the existing baseline has been applied.
	previous.Process.Env = slices.DeleteFunc(previous.Process.Env, func(v string) bool { return v == "TERM=xterm" })
	ptyMount := spec.Mount{Destination: "/dev/pts", Type: "devpts", Source: "devpts", Options: []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"}}
	foundPTY := false
	current.Mounts = slices.DeleteFunc(current.Mounts, func(mount spec.Mount) bool {
		if mount.Destination != "/dev/pts" {
			return false
		}
		if !reflect.DeepEqual(mount, ptyMount) {
			t.Fatalf("unexpected PTY policy: %+v", mount)
		}
		foundPTY = true
		return true
	})
	if !foundPTY {
		t.Fatal("missing isolated PTY mount")
	}
	for _, s := range []*spec.Spec{previous, current} {
		slices.Sort(s.Process.Env)
		for _, caps := range [][]string{s.Process.Capabilities.Bounding, s.Process.Capabilities.Effective, s.Process.Capabilities.Inheritable, s.Process.Capabilities.Permitted} {
			slices.Sort(caps)
		}
	}
	if !reflect.DeepEqual(previous, current) {
		oldJSON, _ := json.Marshal(previous)
		newJSON, _ := json.Marshal(current)
		t.Fatalf("unreviewed effective OCI policy difference:\nprevious=%s\ncurrent=%s", oldJSON, newJSON)
	}
}
