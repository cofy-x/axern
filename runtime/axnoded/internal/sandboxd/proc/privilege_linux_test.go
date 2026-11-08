//go:build linux

package proc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const privilegeProbeCommand = "internal-test-privilege-probe"

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case InternalExecCommand:
			os.Exit(RunInternalExec(os.Args[2:], os.Stderr))
		case privilegeProbeCommand:
			os.Exit(emitPrivilegeProbe())
		}
	}
	os.Exit(m.Run())
}

func emitPrivilegeProbe() int {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 1
	}
	out := map[string]any{"pid": os.Getpid(), "pgrp": syscall.Getpgrp(), "original_env": os.Getenv("AXERN_INERT_CHILD_VALUE")}
	files, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 1
	}
	out["environment_fd_absent"] = true
	for _, file := range files {
		target, _ := os.Readlink("/proc/self/fd/" + file.Name())
		if strings.Contains(target, "axern-child-environment") {
			out["environment_fd_absent"] = false
		}
	}
	for _, line := range strings.Split(string(status), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && (strings.HasPrefix(key, "Cap") || key == "NoNewPrivs" || key == "Uid" || key == "Gid" || key == "Groups") {
			out[key] = strings.Fields(value)
		}
	}
	out["setuid_denied"] = syscall.Setuid(0) == syscall.EPERM
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{{Effective: 1, Permitted: 1}}
	out["capset_denied"] = unix.Capset(&header, &data[0]) == syscall.EPERM
	if len(os.Args) > 2 {
		_, err := os.ReadFile(os.Args[2])
		out["root_file_denied"] = os.IsPermission(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return 1
	}
	return 0
}

func TestPrepareUnprivilegedCommandPreservesProcessIOAndResolvedArgv(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "inert argument")
	cmd.Dir, cmd.Env = "/tmp", []string{"INERT=sentinel"}
	attr := &syscall.SysProcAttr{Setpgid: true}
	cmd.SysProcAttr = attr
	cleanup, err := PrepareUnprivilegedCommand(cmd, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := cmd.ExtraFiles[0].Write([]byte("untrusted replacement")); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("environment not write-sealed: %v", err)
	}
	data, err := io.ReadAll(cmd.ExtraFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	var environment []string
	if json.Unmarshal(data, &environment) != nil || !reflect.DeepEqual(environment, []string{"INERT=sentinel"}) {
		t.Fatalf("original environment was not retained privately")
	}
	want := []string{"/proc/self/exe", InternalExecCommand, "1000", "0", "--", "/bin/sh", "/bin/sh", "-c", "inert argument"}
	if cmd.Path != "/proc/self/exe" || !reflect.DeepEqual(cmd.Args, want) || cmd.SysProcAttr != attr || cmd.Dir != "/tmp" || (cmd.Env == nil || len(cmd.Env) != 0 || len(cmd.ExtraFiles) != 1) {
		t.Fatalf("command lost its existing execution contract: %#v", cmd)
	}
}

func TestInternalExecRejectsMalformedOrRootIdentityWithoutExecuting(t *testing.T) {
	for _, args := range [][]string{nil, {"0", "0", "--", "/bin/false", "false"}, {"bad-identity", "0", "--", "/bin/false", "false"}, {"1000", "0", "bad-boundary", "/bin/false", "false"}} {
		var stderr bytes.Buffer
		if code := RunInternalExec(args, &stderr); code != 126 {
			t.Fatalf("bad request returned %d", code)
		}
		if strings.Contains(stderr.String(), "bad-identity") || strings.Contains(stderr.String(), "/bin/false") {
			t.Fatal("internal failure exposed caller input")
		}
	}
}

func TestUnprivilegedChildHasZeroCapabilitySetsAndSamePID(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to remove the host child capability bounding set")
	}
	dir, probe := newPrivilegeFixture(t)
	private := filepath.Join(dir, "root-only")
	if err := os.WriteFile(private, []byte("inert private fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	parentBefore, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, gid := range []uint32{65534, 0} {
		t.Run(fmt.Sprint(gid), func(t *testing.T) {
			cmd := exec.Command(probe, privilegeProbeCommand, private)
			cmd.Env = []string{"PATH=/usr/bin:/bin", "AXERN_INERT_CHILD_VALUE=restored-value"}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cleanup, err := PrepareUnprivilegedCommand(cmd, 65534, gid)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			output, err := cmd.Output()
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					t.Fatalf("unprivileged child: %v: %s", err, exit.Stderr)
				}
				t.Fatal(err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(output, &got); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"CapBnd", "CapEff", "CapPrm", "CapInh", "CapAmb"} {
				if string(got[field]) != `["0000000000000000"]` {
					t.Fatalf("%s was not empty: %s", field, got[field])
				}
			}
			for _, field := range []string{"setuid_denied", "capset_denied", "root_file_denied", "environment_fd_absent"} {
				if string(got[field]) != "true" {
					t.Fatalf("%s not proven: %s", field, output)
				}
			}
			for field, id := range map[string]string{"Uid": "65534", "Gid": strconv.FormatUint(uint64(gid), 10)} {
				want, _ := json.Marshal([]string{id, id, id, id})
				if !bytes.Equal(got[field], want) {
					t.Fatalf("%s = %s", field, got[field])
				}
			}
			if string(got["original_env"]) != `"restored-value"` {
				t.Fatalf("target environment lost: %s", output)
			}
			if string(got["Groups"]) != "[]" || string(got["NoNewPrivs"]) != `["1"]` || string(got["pid"]) != strconv.Itoa(cmd.Process.Pid) || string(got["pgrp"]) != strconv.Itoa(cmd.Process.Pid) {
				t.Fatalf("child identity, PID or inherited policy changed: %s", output)
			}
		})
	}
	parentAfter, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"CapBnd:", "CapEff:", "CapPrm:", "CapInh:", "CapAmb:", "NoNewPrivs:"} {
		before, after := "", ""
		for _, line := range strings.Split(string(parentBefore), "\n") {
			if strings.HasPrefix(line, field) {
				before = line
			}
		}
		for _, line := range strings.Split(string(parentAfter), "\n") {
			if strings.HasPrefix(line, field) {
				after = line
			}
		}
		if before != after {
			t.Fatalf("parent %s changed", field)
		}
	}
}

func newPrivilegeFixture(t *testing.T) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "axern-child-privilege-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe.test")
	if err := os.WriteFile(probe, data, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(probe, 0755); err != nil {
		t.Fatal(err)
	}
	return dir, probe
}

func TestChildStopsWhenBoundingAuthorityIsMissing(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to create a constrained child identity")
	}
	bounding, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, unix.CAP_CHOWN, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bounding == 0 {
		t.Skip("the host already removed the bounding capability used by this negative fixture")
	}
	_, trusted := newPrivilegeFixture(t)
	cmd := exec.Command("/bin/sh", "-c", "printf INERT_TARGET_MUST_NOT_RUN")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cleanup, err := PrepareUnprivilegedCommand(cmd, 65534, 65534)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	// The trusted copied test image has explicit main dispatch. Starting it
	// after a normal Unix UID transition leaves the old bounding set present
	// but removes the privilege necessary to reduce it.
	cmd.Path = trusted
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 126 || !bytes.Contains(output, []byte("clear capability bounding set")) || bytes.Contains(output, []byte("INERT_TARGET_MUST_NOT_RUN")) {
		t.Fatalf("missing bounding authority did not stop before target execution: %v, %s", err, output)
	}
}

func TestChildCannotBorrowRootWorkingDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to create a private working directory")
	}
	cmd := exec.Command("/bin/sh", "-c", "printf INERT_TARGET_MUST_NOT_RUN")
	cmd.Dir = t.TempDir()
	if err := os.Chmod(cmd.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cleanup, err := PrepareUnprivilegedCommand(cmd, 65534, 65534)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 126 || !bytes.Contains(output, []byte("validate unprivileged working directory")) || bytes.Contains(output, []byte("INERT_TARGET_MUST_NOT_RUN")) {
		t.Fatalf("private cwd did not stop before target execution: %v, %s", err, output)
	}
}

func TestChildCannotBorrowRootAncestorWorkingDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to create an untraversable ancestor")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(parent, "public-child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(child, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "data"), []byte("inert untraversable-parent fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", "cat data; printf INERT_TARGET_MUST_NOT_RUN")
	cmd.Dir, cmd.Env = child, []string{"PATH=/usr/bin:/bin"}
	cleanup, err := PrepareUnprivilegedCommand(cmd, 65534, 65534)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 126 || !bytes.Contains(output, []byte("validate unprivileged working directory")) || bytes.Contains(output, []byte("INERT_TARGET_MUST_NOT_RUN")) || bytes.Contains(output, []byte("inert untraversable-parent fixture")) {
		t.Fatalf("private ancestor did not stop target: %v, %s", err, output)
	}
}
