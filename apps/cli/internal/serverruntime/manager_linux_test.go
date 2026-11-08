//go:build linux

package serverruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fixtureFile struct {
	data []byte
	mode os.FileMode
	uid  uint32
}

func (f fixtureFile) Name() string       { return "fixture" }
func (f fixtureFile) Size() int64        { return int64(len(f.data)) }
func (f fixtureFile) Mode() os.FileMode  { return f.mode }
func (f fixtureFile) ModTime() time.Time { return time.Time{} }
func (f fixtureFile) IsDir() bool        { return f.mode.IsDir() }
func (f fixtureFile) Sys() any           { return &syscall.Stat_t{Uid: f.uid} }

type fixtureFiles map[string]fixtureFile

func (f fixtureFiles) Lstat(path string) (os.FileInfo, error) {
	v, ok := f[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return v, nil
}
func (f fixtureFiles) ReadFile(path string) ([]byte, error) {
	v, ok := f[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return v.data, nil
}
func (f fixtureFiles) put(path string, data []byte) {
	f[path] = fixtureFile{data: data, mode: 0644}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		f[parent] = fixtureFile{mode: os.ModeDir | 0755}
		if parent == filepath.Dir(parent) {
			break
		}
	}
}

type recordingRunner struct {
	program string
	args    []string
	calls   int
	failure error
}

func (r *recordingRunner) Run(_ context.Context, _, _ io.Writer, program string, args ...string) error {
	r.program, r.args, r.calls = program, append([]string(nil), args...), r.calls+1
	return r.failure
}

func serverFixture(t *testing.T) (Manager, fixtureFiles, *recordingRunner, string) {
	t.Helper()
	files, runner := fixtureFiles{}, &recordingRunner{}
	digest := strings.Repeat("a", 64)
	release := "/opt/axern/releases/" + digest
	helper := release + "/" + helperRelativePath
	data := []byte("# trusted installed fixture; never executed\n")
	checksum := sha256.Sum256(data)
	files.put("/etc/axern/installation.json", []byte(`{"schema":"axern.native-install.v1","archive_sha256":"`+digest+`","source_revision":"revision"}`))
	files.put("/etc/axern/installed", []byte(digest+"\n"))
	manifest, err := json.Marshal(map[string]any{"schema": "axern.native-runtime.v2", "os": "linux", "source_revision": "revision", "files": []map[string]any{{"path": helperRelativePath, "sha256": hex.EncodeToString(checksum[:]), "size": len(data)}}})
	if err != nil {
		t.Fatal(err)
	}
	files.put(release+"/manifest.json", manifest)
	files.put(helper, data)
	return Manager{runner: runner, files: files, stdout: io.Discard, stderr: io.Discard, osName: "linux"}, files, runner, helper
}

func TestOperateDelegatesOnlyToVerifiedInstalledHelper(t *testing.T) {
	for _, operation := range []string{"start", "stop", "status", "renew-services"} {
		t.Run(operation, func(t *testing.T) {
			manager, _, runner, helper := serverFixture(t)
			if err := manager.Operate(t.Context(), operation); err != nil {
				t.Fatal(err)
			}
			if runner.program != "/usr/bin/python3" || !reflect.DeepEqual(runner.args, []string{"-I", helper, operation}) {
				t.Fatalf("unexpected helper command: %s %q", runner.program, runner.args)
			}
		})
	}
}

func TestInvalidInstallationNeverExecutesHelper(t *testing.T) {
	cases := map[string]func(fixtureFiles, string){
		"different-marker": func(f fixtureFiles, _ string) {
			f["/etc/axern/installed"] = fixtureFile{data: []byte(strings.Repeat("b", 64)), mode: 0644}
		},
		"invalid-digest-path": func(f fixtureFiles, _ string) {
			f["/etc/axern/installation.json"] = fixtureFile{data: []byte(`{"schema":"axern.native-install.v1","archive_sha256":"../../elsewhere"}`), mode: 0644}
		},
		"missing-marker":  func(f fixtureFiles, _ string) { delete(f, "/etc/axern/installed") },
		"writable-parent": func(f fixtureFiles, _ string) { f["/opt/axern"] = fixtureFile{mode: os.ModeDir | 0777} },
		"symlink-parent":  func(f fixtureFiles, _ string) { f["/opt/axern"] = fixtureFile{mode: os.ModeSymlink | 0755} },
		"wrong-owner": func(f fixtureFiles, _ string) {
			v := f["/etc/axern/installation.json"]
			v.uid = 1000
			f["/etc/axern/installation.json"] = v
		},
		"symlink-helper": func(f fixtureFiles, helper string) { v := f[helper]; v.mode = os.ModeSymlink | 0777; f[helper] = v },
		"changed-helper": func(f fixtureFiles, helper string) {
			v := f[helper]
			v.data = []byte("unexpected helper")
			f[helper] = v
		},
		"wrong-manifest": func(f fixtureFiles, helper string) {
			f[filepath.Dir(filepath.Dir(filepath.Dir(helper)))+"/manifest.json"] = fixtureFile{data: []byte(`{"schema":"wrong"}`), mode: 0644}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manager, files, runner, helper := serverFixture(t)
			mutate(files, helper)
			if err := manager.Operate(t.Context(), "start"); err == nil {
				t.Fatal("unverified installation accepted")
			}
			if runner.calls != 0 {
				t.Fatal("host tool executed before verification")
			}
		})
	}
}

func TestUnknownOperationAndNonLinuxNeverExecute(t *testing.T) {
	manager, _, runner, _ := serverFixture(t)
	if err := manager.Operate(t.Context(), "reset"); err == nil {
		t.Fatal("destructive operation accepted")
	}
	manager.osName = "darwin"
	if err := manager.Operate(t.Context(), "start"); err == nil {
		t.Fatal("non-Linux operation accepted")
	}
	if runner.calls != 0 {
		t.Fatal("unsupported operation invoked host tool")
	}
}

func TestLogsUseOnlyOwnedUnits(t *testing.T) {
	manager, _, runner, _ := serverFixture(t)
	if err := manager.Logs(t.Context(), LogOptions{Service: "axnoded", Tail: 42, Follow: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"--no-pager", "--output", "short-iso", "--lines", "42", "--unit", "axern-native-axnoded.service", "--follow"}
	if runner.program != "/usr/bin/journalctl" || !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("unexpected journal command: %s %q", runner.program, runner.args)
	}
	for _, options := range []LogOptions{{Service: "ssh.service", Tail: 1}, {Tail: -1}, {Tail: 10001}} {
		if err := manager.Logs(t.Context(), options); err == nil {
			t.Fatalf("invalid log selection accepted: %+v", options)
		}
	}
	if runner.calls != 1 {
		t.Fatal("invalid log selection invoked host tool")
	}
}

func TestHelperFailureIsReturned(t *testing.T) {
	manager, _, runner, _ := serverFixture(t)
	runner.failure = errors.New("helper failed")
	if err := manager.Operate(t.Context(), "stop"); !errors.Is(err, runner.failure) {
		t.Fatalf("helper failure lost: %v", err)
	}
}
