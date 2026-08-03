package localruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/config"
)

func TestResetRemovesRootOwnedDataWithLockedHelperImage(t *testing.T) {
	manager, runner := newResetTestManager(t)
	removeCalls := 0
	manager.removeAll = func(path string) error {
		removeCalls++
		if removeCalls == 1 {
			return os.ErrPermission
		}
		return os.RemoveAll(path)
	}

	if err := manager.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if removeCalls != 2 {
		t.Fatalf("remove calls = %d, want 2", removeCalls)
	}
	if _, err := os.Stat(manager.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local directory still exists: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("runner calls = %#v, want compose down and cleanup helper", runner.calls)
	}
	wantHelper := []string{
		"docker", "run", "--rm", "--network", "none", "--read-only", "--user", "0:0",
		"--entrypoint", "/bin/sh", "-v", manager.Dir + ":/source", "example.invalid/postgres@sha256:locked", "-c",
	}
	if len(runner.calls[1]) < len(wantHelper) {
		t.Fatalf("cleanup helper = %#v, want prefix %#v", runner.calls[1], wantHelper)
	}
	if got := runner.calls[1][:len(wantHelper)]; !reflect.DeepEqual(got, wantHelper) {
		t.Fatalf("cleanup helper = %#v, want prefix %#v", runner.calls[1], wantHelper)
	}
	cfg, err := config.Load(manager.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Contexts[ContextName]; ok || cfg.CurrentContext == ContextName {
		t.Fatalf("local context was not removed: %#v", cfg)
	}
}

func TestResetKeepsContextWhenRootCleanupFails(t *testing.T) {
	manager, runner := newResetTestManager(t)
	manager.removeAll = func(string) error { return os.ErrPermission }
	runner.helperErr = errors.New("cleanup helper failed")

	err := manager.Reset(context.Background())
	if err == nil || !strings.Contains(err.Error(), "remove root-owned local data") {
		t.Fatalf("Reset() error = %v", err)
	}
	cfg, loadErr := config.Load(manager.ConfigPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if _, ok := cfg.Contexts[ContextName]; !ok || cfg.CurrentContext != ContextName {
		t.Fatalf("local context changed after failed cleanup: %#v", cfg)
	}
}

func TestResetRejectsUnexpectedAndSymlinkedPathsBeforeDocker(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AXERN_HOME", root)
	for _, test := range []struct {
		name string
		dir  func(*testing.T) string
		want string
	}{
		{
			name: "unexpected path",
			dir: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), ContextName)
			},
			want: "unexpected local path",
		},
		{
			name: "symlinked path",
			dir: func(t *testing.T) string {
				target := filepath.Join(t.TempDir(), ContextName)
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, ContextName)
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
				return path
			},
			want: "symlinked local path",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &resetRunner{}
			manager := &Manager{Dir: test.dir(t), ConfigPath: filepath.Join(root, "config.json"), Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
			err := manager.Reset(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Reset() error = %v, want %q", err, test.want)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("unsafe reset invoked Docker: %#v", runner.calls)
			}
		})
	}
}

func TestResetRejectsConfigInsideLocalData(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AXERN_HOME", root)
	dir := filepath.Join(root, ContextName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &resetRunner{}
	manager := &Manager{Dir: dir, ConfigPath: filepath.Join(dir, "config.json"), Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	err := manager.Reset(context.Background())
	if err == nil || !strings.Contains(err.Error(), "containing Axern config") {
		t.Fatalf("Reset() error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unsafe reset invoked Docker: %#v", runner.calls)
	}
}

func newResetTestManager(t *testing.T) (*Manager, *resetRunner) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AXERN_HOME", root)
	dir := filepath.Join(root, ContextName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.env"), []byte("POSTGRES_IMAGE=\"example.invalid/postgres@sha256:locked\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &resetRunner{}
	manager := &Manager{Version: "0.4.0", Dir: dir, ConfigPath: filepath.Join(root, "config.json"), Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	if err := manager.writeContext(true); err != nil {
		t.Fatal(err)
	}
	return manager, runner
}

type resetRunner struct {
	calls     [][]string
	helperErr error
}

func (r *resetRunner) Run(_ context.Context, _, _ io.Writer, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(args) > 0 && args[0] == "run" {
		return r.helperErr
	}
	return nil
}

func (*resetRunner) Output(context.Context, string, ...string) ([]byte, error) { return nil, nil }
