// Copyright 2026 The Axern Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hostlinux

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunscProcessRole(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"sentry", []string{"runsc-sandbox", "--root=/run/gofer", "--debug=true", "--log-fd=3", "boot", "--gofer-mount-confs=lisafs:none", "--setup-root", "--cpu-num", "4"}, "boot"},
		{"gofer", []string{"runsc-gofer", "--root=/run/sandbox", "--strace=false", "--log-fd=3", "gofer", "--bundle", "/bundle"}, "gofer"},
		{"title alone", []string{"runsc-gofer"}, ""},
		{"flag is not a command", []string{"runsc-gofer", "--gofer-mount-confs=gofer"}, ""},
		{"flag value is not a command", []string{"runsc-gofer", "--log=gofer", "start", "id"}, ""},
		{"later argument is not a command", []string{"runsc-gofer", "start", "gofer"}, ""},
		{"path is not a command", []string{"runsc-gofer", "/tmp/gofer"}, ""},
		{"path is not a title", []string{"/tmp/runsc-gofer", "gofer"}, ""},
		{"title suffix is not a role", []string{"runsc-gofer-helper", "gofer"}, ""},
		{"boot title cannot claim gofer", []string{"runsc-sandbox", "gofer"}, ""},
		{"gofer title cannot claim boot", []string{"runsc-gofer", "boot"}, ""},
		{"case is exact", []string{"runsc-gofer", "GOFER"}, ""},
		{"unexpected global value syntax", []string{"runsc-gofer", "--log", "gofer"}, ""},
		{"option separator", []string{"runsc-gofer", "--", "gofer"}, ""},
		{"empty argument", []string{"runsc-gofer", "", "gofer"}, ""},
		{"cleared systrap argv", nil, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmdline := []byte(strings.Join(test.args, "\x00") + "\x00")
			if got := runscProcessRole(cmdline); got != test.want {
				t.Fatalf("runscProcessRole(%q) = %q, want %q", cmdline, got, test.want)
			}
		})
	}
	if got := runscProcessRole([]byte("runsc-gofer\x00gofer")); got != "" {
		t.Fatalf("unterminated cmdline accepted as %q", got)
	}
}

func TestVerifyRunscProcessIdentities(t *testing.T) {
	type process struct {
		pid  int
		args []string
		exe  string
	}
	sentryArgs := []string{"runsc-sandbox", "--root=/state", "boot", "--gofer-mount-confs=lisafs:anon", "--gofer-filestore-fds=9", "allocation"}
	sentry := process{10, sentryArgs, "sentry"}
	gofer := process{11, []string{"runsc-gofer", "--root=/state", "gofer", "--bundle", "/bundle", "allocation"}, "runsc"}
	clone := process{12, sentryArgs, "sentry"}
	for _, test := range []struct {
		name      string
		processes []process
		wantError string
	}{
		{"pinned roles", []process{sentry, gofer}, ""},
		{"systrap clone inherits boot argv", []process{sentry, gofer, clone}, ""},
		{"systrap clone has cleared argv", []process{sentry, gofer, {12, nil, "sentry"}}, ""},
		{"proc umounter is not a role", []process{sentry, gofer, {13, []string{"gvisor-proc-umounter", "/proc"}, "other"}}, ""},
		{"sentry flags cannot replace gofer", []process{sentry}, "sentry=true gofer=false"},
		{"clone cannot replace gofer", []process{sentry, clone}, "sentry=true gofer=false"},
		{"clone cannot replace saved sentry", []process{gofer, clone}, "sentry=false gofer=true"},
		{"path cannot replace gofer", []process{sentry, {11, []string{"/tmp/runsc-gofer", "gofer"}, "runsc"}}, "sentry=true gofer=false"},
		{"later argument cannot replace gofer", []process{sentry, {11, []string{"runsc-gofer", "start", "gofer"}, "runsc"}}, "sentry=true gofer=false"},
		{"wrong sentry inode", []process{{10, sentry.args, "other"}, gofer}, "executable identity differs"},
		{"wrong gofer inode", []process{sentry, {11, gofer.args, "other"}}, "executable identity differs"},
		{"sentry binary cannot claim gofer", []process{sentry, {11, gofer.args, "sentry"}}, "executable identity differs"},
		{"extra false gofer is rejected", []process{sentry, gofer, {13, gofer.args, "other"}}, "executable identity differs"},
		{"gofer exits before executable read", []process{sentry, {11, gofer.args, "missing"}}, "stat runsc process 11 executable"},
		{"saved sentry pid cannot count as gofer", []process{{10, gofer.args, "runsc"}, clone}, "sentry=false gofer=false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			runtimeBinary := filepath.Join(root, "runsc")
			binaries := map[string]string{"runsc": runtimeBinary, "sentry": RunscSentryBinary(runtimeBinary), "other": filepath.Join(root, "other")}
			for name, path := range binaries {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(name), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			procRoot := filepath.Join(root, "proc")
			pids := make(map[int]struct{}, len(test.processes))
			for _, process := range test.processes {
				pids[process.pid] = struct{}{}
				dir := filepath.Join(procRoot, strconv.Itoa(process.pid))
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(process.args, "\x00")+"\x00"), 0o600); err != nil {
					t.Fatal(err)
				}
				if process.exe != "missing" {
					if err := os.Symlink(binaries[process.exe], filepath.Join(dir, "exe")); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := verifyRunscProcessIdentities(procRoot, pids, 10, runtimeBinary)
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("verifyRunscProcessIdentities() = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}
