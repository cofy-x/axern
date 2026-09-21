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
	"testing"
)

func TestRunscSentryBinary(t *testing.T) {
	got := RunscSentryBinary("/opt/axern/bin/runsc")
	want := "/opt/axern/bin/gvisor-bin/gvisor_sentry"
	if got != want {
		t.Fatalf("RunscSentryBinary() = %q, want %q", got, want)
	}
}

func TestVerifyProcessExecutable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProcessExecutable(os.Getpid(), executable); err != nil {
		t.Fatalf("VerifyProcessExecutable() = %v", err)
	}

	mismatch := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(mismatch, []byte("not this process"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProcessExecutable(os.Getpid(), mismatch); err == nil {
		t.Fatal("VerifyProcessExecutable() succeeded for a different inode")
	}
}
