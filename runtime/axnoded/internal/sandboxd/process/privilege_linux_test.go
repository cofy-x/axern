//go:build linux

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == proc.InternalExecCommand {
		os.Exit(proc.RunInternalExec(os.Args[2:], os.Stderr))
	}
	os.Exit(m.Run())
}

func TestNonrootRegistryPipeAndPTYUseEmptyCapabilities(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to remove the host child capability bounding set")
	}
	for _, terminal := range []bool{false, true} {
		for _, gid := range []uint32{65534, 0} {
			t.Run(fmt.Sprintf("terminal-%v-gid-%d", terminal, gid), func(t *testing.T) {
				waiter := proc.NewWaiter(context.Background())
				defer waiter.Stop()
				registry := NewRegistry(waiter, nil, "/")
				// Never project the host test runner's credentials/environment.
				registry.env = []string{"PATH=/usr/bin:/bin"}
				started, err := registry.Start(StartRequest{
					Args: []string{"/bin/sh", "-c", "exec /bin/cat /proc/self/status"},
					User: fmt.Sprintf("65534:%d", gid), Cwd: "/", Terminal: terminal, CaptureOutput: true,
				})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				result, ok, err := registry.Wait(ctx, started.ID)
				if err != nil || !ok || result.ExitCode == nil || *result.ExitCode != 0 {
					t.Fatalf("process failed: %+v, %v", result, err)
				}
				status := map[string][]string{}
				for _, line := range strings.Split(result.Stdout, "\n") {
					key, value, found := strings.Cut(line, ":")
					if found {
						status[key] = strings.Fields(value)
					}
				}
				for _, name := range []string{"CapBnd", "CapEff", "CapPrm", "CapInh", "CapAmb"} {
					if strings.Join(status[name], " ") != "0000000000000000" {
						t.Fatalf("%s not empty: %s", name, result.Stdout)
					}
				}
				if strings.Join(status["Uid"], " ") != "65534 65534 65534 65534" || strings.Join(status["Gid"], " ") != fmt.Sprintf("%d %d %d %d", gid, gid, gid, gid) || len(status["Groups"]) != 0 || strings.Join(status["NoNewPrivs"], " ") != "1" {
					t.Fatalf("user or privilege identity mismatch: %s", result.Stdout)
				}
				if strings.Join(status["Pid"], " ") != fmt.Sprint(started.PID) {
					t.Fatalf("child exec changed tracked PID: %s", result.Stdout)
				}
			})
		}
	}
}

func TestExplicitRootUserKeepsSetupAuthority(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root for explicit setup identity")
	}
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	registry := NewRegistry(waiter, nil, "/")
	registry.env = []string{"PATH=/usr/bin:/bin"}
	fixture := t.TempDir() + "/owned-file"
	if err := os.WriteFile(fixture, []byte("inert setup fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	started, err := registry.Start(StartRequest{Args: []string{"/bin/chown", "65534:65534", fixture}, User: "0:0", CaptureOutput: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, ok, err := registry.Wait(ctx, started.ID)
	if err != nil || !ok || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("root setup failed: %+v, %v", result, err)
	}
}

func TestUnprivilegedStartFailureClosesPrivateEnvironmentFD(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			cmd := exec.Command("/bin/true")
			cmd.Dir = t.TempDir() + "/missing"
			cmd.Env = []string{"INERT_FIXTURE=value"}
			user := processUser{uid: 65534, gid: 65534}
			var err error
			if terminal {
				_, err = startTerminalProcess(cmd, nil, user, true)
			} else {
				err = startProcessGroupWithUser(cmd, user, true)
			}
			if err == nil {
				t.Fatal("invalid working directory unexpectedly started")
			}
			if len(cmd.ExtraFiles) != 1 {
				t.Fatal("private environment descriptor was not prepared")
			}
			if _, err := cmd.ExtraFiles[0].Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("parent retained the private environment descriptor: %v", err)
			}
		})
	}
}

func TestNonrootPipeAndPTYRejectUntraversableCwdAncestor(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to create a root-owned ancestor")
	}
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
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
			if err := os.WriteFile(filepath.Join(child, "data"), []byte("inert parent-protected data"), 0644); err != nil {
				t.Fatal(err)
			}
			waiter := proc.NewWaiter(context.Background())
			defer waiter.Stop()
			registry := NewRegistry(waiter, nil, "/")
			registry.env = []string{"PATH=/usr/bin:/bin"}
			started, err := registry.Start(StartRequest{Args: []string{"/bin/sh", "-c", "cat data; printf INERT_TARGET_MUST_NOT_RUN"}, Cwd: child, User: "65534:0", Terminal: terminal, CaptureOutput: true})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, ok, err := registry.Wait(ctx, started.ID)
			if err != nil || !ok || result.ExitCode == nil || *result.ExitCode != 126 || strings.Contains(result.Stdout+result.Stderr, "INERT_TARGET_MUST_NOT_RUN") || strings.Contains(result.Stdout+result.Stderr, "inert parent-protected data") || !strings.Contains(result.Stdout+result.Stderr, "validate unprivileged working directory") {
				t.Fatalf("private ancestor did not stop target: %+v, %v", result, err)
			}
		})
	}
}
