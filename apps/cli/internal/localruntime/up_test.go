package localruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/localbundle"
)

func TestUpRetriesUnappliedBundleAfterMaterialization(t *testing.T) {
	t.Setenv(localDNSNameserversEnv, "192.0.2.53")
	dockerDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dockerDir, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dockerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	node, err := json.Marshal(readyLocalNodePayload(time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	http.DefaultTransport = localUpTransport(func(request *http.Request) (*http.Response, error) {
		body := []byte("ok")
		if request.URL.Path == "/nodesz" {
			body = node
		} else if request.URL.Path != "/healthz" {
			return nil, fmt.Errorf("unexpected readiness request: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})

	for _, previousDigest := range []string{"", "previous-successful-bundle"} {
		t.Run("previous-digest="+previousDigest, func(t *testing.T) {
			root := t.TempDir()
			runner := &bundleRetryRunner{failPull: true}
			manager := &Manager{Version: "dev", Dir: filepath.Join(root, "local"), ConfigPath: filepath.Join(root, "config.json"), Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
			if err := manager.materialize(nil); err != nil {
				t.Fatal(err)
			}
			oldCompose := append(bytes.Clone(localbundle.Compose), []byte("\n  obsolete-worker:\n    image: example.invalid/worker:dev\n")...)
			if err := os.WriteFile(manager.composePath(), oldCompose, 0o600); err != nil {
				t.Fatal(err)
			}
			createdAt := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
			if err := saveMetadata(manager.metadataPath(), Metadata{Version: "dev", ComposeProject: ProjectName, AppliedBundleDigest: previousDigest, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
				t.Fatal(err)
			}
			checkApplied := func(want string) {
				t.Helper()
				metadata, err := loadMetadata(manager.metadataPath())
				if err != nil {
					t.Fatal(err)
				}
				if metadata.AppliedBundleDigest != want || !metadata.CreatedAt.Equal(createdAt) {
					t.Fatalf("applied bundle metadata = %+v, want digest %q and original creation time", metadata, want)
				}
			}
			options := UpOptions{ReadinessTimeout: time.Second}
			if err := manager.Up(context.Background(), options); !errors.Is(err, errBundlePull) {
				t.Fatalf("first Up = %v, want pull failure after materialization", err)
			}
			if manager.localBundleChanged() {
				t.Fatal("failed Up did not materialize the desired bundle")
			}
			checkApplied(previousDigest)

			// The old stack stays healthy after the pull failure. A retry must
			// still reconcile services, then wait for the final DNS check.
			runner.failPull, runner.failDNS = false, true
			if err := manager.Up(context.Background(), options); err == nil || !strings.Contains(err.Error(), "runtime_dns_node_unreachable") {
				t.Fatalf("retry Up = %v, want final DNS failure", err)
			}
			if runner.pulls != 2 || runner.reconciliations != 1 {
				t.Fatalf("retry skipped service reconciliation: pulls=%d reconciliations=%d", runner.pulls, runner.reconciliations)
			}
			checkApplied(previousDigest)

			runner.failDNS = false
			if err := manager.Up(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			if runner.pulls != 3 || runner.reconciliations != 2 {
				t.Fatalf("successful retry skipped service reconciliation: pulls=%d reconciliations=%d", runner.pulls, runner.reconciliations)
			}
			checkApplied(localBundleDigest())

			calls := runner.runs
			if err := manager.Up(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			if runner.runs != calls {
				t.Fatal("successfully applied healthy bundle did not use the fast path")
			}
		})
	}
}

type localUpTransport func(*http.Request) (*http.Response, error)

func (f localUpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

var errBundlePull = errors.New("test registry unavailable")

type bundleRetryRunner struct {
	failPull        bool
	failDNS         bool
	pulls           int
	reconciliations int
	runs            int
}

func (r *bundleRetryRunner) Run(_ context.Context, _, _ io.Writer, name string, args ...string) error {
	r.runs++
	if name != "docker" || len(args) == 0 || args[0] != "compose" {
		return fmt.Errorf("unexpected run: %s %v", name, args)
	}
	if slices.Contains(args, "pull") {
		r.pulls++
		if r.failPull {
			return errBundlePull
		}
	}
	if slices.Contains(args, "--remove-orphans") {
		r.reconciliations++
	}
	return nil
}

func (r *bundleRetryRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "docker" {
		return nil, fmt.Errorf("unexpected output command: %s %v", name, args)
	}
	switch {
	case slices.Equal(args, []string{"info", "--format", "{{.ServerVersion}} {{.Architecture}} {{.MemTotal}}"}):
		return []byte("28.0.0 " + runtime.GOARCH + " 8589934592"), nil
	case slices.Equal(args, []string{"compose", "version", "--short"}):
		return []byte("2.32.4"), nil
	case len(args) > 1 && args[0] == "network" && args[1] == "ls":
		return []byte(RegistryNetworkName), nil
	case len(args) > 1 && args[0] == "network" && args[1] == "inspect":
		return []byte("bridge|local|true"), nil
	case len(args) > 0 && args[0] == "compose" && slices.Contains(args, "ps"):
		return []byte(`[{"Service":"controld","State":"running","Health":"healthy"}]`), nil
	case len(args) > 0 && args[0] == "compose" && slices.Contains(args, "/usr/local/libexec/axnoded/dns-probe"):
		if r.failDNS {
			return []byte(`{"status":"fail","code":"runtime_dns_node_unreachable","effective_resolver_count":1,"successful_resolver_count":0}`), nil
		}
		return []byte(`{"status":"pass","code":"runtime_dns_node_reachable","effective_resolver_count":1,"successful_resolver_count":1}`), nil
	default:
		return nil, fmt.Errorf("unexpected Docker output: %v", args)
	}
}
