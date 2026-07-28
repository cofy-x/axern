package ocihost

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOCICommonEnsureContainerPath(t *testing.T) {
	root := t.TempDir()
	common := &Common{
		containerRoot: filepath.Join(root, "containers"),
	}

	err := common.EnsureContainerPath("axctl-test")
	if err != nil {
		t.Fatalf("EnsureContainerPath() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "containers", "axctl-test"))
	if err != nil {
		t.Fatalf("stat container path: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected container path to be a directory")
	}
}

func TestRuntimeExitStatePathIsOutsideOCIRuntimeRoot(t *testing.T) {
	root := t.TempDir()
	common, err := New(Config{
		Root:        root,
		RuntimeName: "runc",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got := common.RuntimeExitStatePath("axctl-test")
	want := filepath.Join(root, runtimeExitStateStoreDirName, "runc", "axctl-test.json")
	if got != want {
		t.Fatalf("RuntimeExitStatePath = %q, want %q", got, want)
	}
}

func TestRemoveExitStateIsIdempotent(t *testing.T) {
	root := t.TempDir()
	common, err := New(Config{Root: root, RuntimeName: "runc"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	path := common.RuntimeExitStatePath("axctl-test")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir exit state root: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatalf("write exit state: %v", err)
	}

	if err := common.RemoveExitState("axctl-test"); err != nil {
		t.Fatalf("RemoveExitState() error = %v", err)
	}
	if err := common.RemoveExitState("axctl-test"); err != nil {
		t.Fatalf("second RemoveExitState() error = %v", err)
	}
}

func TestNewOCICommonRemovesRuntimeRootExitStateDir(t *testing.T) {
	root := t.TempDir()
	runtimeRootExitStateDir := filepath.Join(root, "runc", runtimeExitStateStoreDirName)
	if err := os.MkdirAll(runtimeRootExitStateDir, 0755); err != nil {
		t.Fatalf("mkdir runtime-root exit state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRootExitStateDir, "old.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("write runtime-root exit state: %v", err)
	}

	if _, err := New(Config{Root: root, RuntimeName: "runc"}); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := os.Stat(runtimeRootExitStateDir); !os.IsNotExist(err) {
		t.Fatalf("runtime-root exit state dir stat error = %v, want not exist", err)
	}
}

func TestParseWaitExitCode(t *testing.T) {
	code, err := ParseWaitExitCode([]byte("0\n"))
	assert.NoError(t, err)
	assert.Equal(t, 0, code)

	code, err = ParseWaitExitCode([]byte(`{"exitStatus":7}`))
	assert.NoError(t, err)
	assert.Equal(t, 7, code)

	code, err = ParseWaitExitCode([]byte(`{"exitCode":0}`))
	assert.NoError(t, err)
	assert.Equal(t, 0, code)
}

func TestParseOCIContainerListOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		wantIDs []string
		wantErr bool
	}{
		{
			name:   "empty",
			output: "",
		},
		{
			name:   "null",
			output: "null",
		},
		{
			name:    "json array",
			output:  `[{"id":"axctl-a","pid":12,"status":"running","bundle":"/tmp/a","created":"2026-05-01T00:00:00Z"}]`,
			wantIDs: []string{"axctl-a"},
		},
		{
			name:    "diagnostic prefix",
			output:  fmt.Sprintf("load container %s: container does not exist\n[{\"id\":\"axctl-b\",\"pid\":13,\"status\":\"created\",\"bundle\":\"/tmp/b\",\"created\":\"2026-05-01T00:00:00Z\"}]\n", runtimeExitStateStoreDirName),
			wantIDs: []string{"axctl-b"},
		},
		{
			name:    "diagnostic suffix",
			output:  "[{\"id\":\"axctl-c\",\"pid\":14,\"status\":\"stopped\",\"bundle\":\"/tmp/c\",\"created\":\"2026-05-01T00:00:00Z\"}]\nwarning after json\n",
			wantIDs: []string{"axctl-c"},
		},
		{
			name:    "no json array",
			output:  `time="2026-05-01T08:10:48Z" level=error msg="open /run/runc: no such file or directory"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseContainerListOutput([]byte(tt.output))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseContainerListOutput() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseContainerListOutput() error = %v", err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("parseContainerListOutput() got %d containers, want %d", len(got), len(tt.wantIDs))
			}
			for i := range tt.wantIDs {
				if got[i].ID != tt.wantIDs[i] {
					t.Fatalf("container[%d].ID = %q, want %q", i, got[i].ID, tt.wantIDs[i])
				}
			}
		})
	}
}
