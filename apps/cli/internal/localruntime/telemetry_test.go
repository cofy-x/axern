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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/localbundle"
)

func TestRetireBundledTelemetryPreservesDevelopmentDataAndRetries(t *testing.T) {
	t.Setenv(localDNSNameserversEnv, "192.0.2.53")
	t.Setenv("AXERN_OTEL_ENABLED", "false")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
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

	for _, failCleanup := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup-failure=%t", failCleanup), func(t *testing.T) {
			root := t.TempDir()
			manager := &Manager{Version: "dev", Dir: filepath.Join(root, "local"), ConfigPath: filepath.Join(root, "config.json"), Stdout: io.Discard, Stderr: io.Discard}
			if err := manager.materialize([]string{"forge.example:5000"}); err != nil {
				t.Fatal(err)
			}
			oldCompose := append(bytes.Clone(localbundle.Compose), []byte("\n  otel-collector:\n    image: example.invalid/collector:old\n    profiles: [observability]\n  otel-lgtm:\n    image: example.invalid/lgtm:old\n    profiles: [observability]\n")...)
			if err := os.WriteFile(manager.composePath(), oldCompose, 0o600); err != nil {
				t.Fatal(err)
			}
			collectorPath := filepath.Join(manager.Dir, "otel-collector.yaml")
			if err := os.WriteFile(collectorPath, []byte("previous collector configuration"), 0o600); err != nil {
				t.Fatal(err)
			}
			createdAt := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
			oldMetadata := Metadata{Version: "dev", ComposeProject: ProjectName, Profile: "observability", CreatedAt: createdAt, UpdatedAt: createdAt, ExternalInsecureRegistries: []string{"forge.example:5000"}}
			if err := saveMetadata(manager.metadataPath(), oldMetadata); err != nil {
				t.Fatal(err)
			}
			remoteContext := []byte(`{"current_context":"remote","contexts":{"remote":{"endpoint":"remote.example:443","proxy_mode":"direct","tls":{"ca_cert":"/operator/ca.pem","cert":"/operator/client.pem","key":"/operator/client-key.pem"}}}}`)
			if err := os.WriteFile(manager.ConfigPath, remoteContext, 0o600); err != nil {
				t.Fatal(err)
			}
			retained := map[string][]byte{}
			for _, name := range []string{"data/postgres/retained", "data/axnoded/retained"} {
				path := filepath.Join(manager.Dir, name)
				if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"data/postgres/retained", "data/axnoded/retained", "secrets.json", "enrollment-token", "certs/ca.crt", "ssh/gateway_host_ed25519"} {
				path := filepath.Join(manager.Dir, name)
				retained[path], err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			runner := &telemetryMigrationRunner{manager: manager, oldCompose: oldCompose, activeTelemetry: true, failCleanup: failCleanup}
			runner.failPull = true
			manager.Runner = runner
			options := UpOptions{ReadinessTimeout: time.Second}
			err = manager.Up(context.Background(), options)
			if failCleanup {
				if !errors.Is(err, errTelemetryCleanup) {
					t.Fatalf("Up = %v, want cleanup failure", err)
				}
				if runner.pulls != 0 || !bytes.Equal(mustReadTelemetryFile(t, manager.composePath()), oldCompose) {
					t.Fatal("failed telemetry cleanup replaced the old bundle or started the core")
				}
				if _, err := os.Stat(collectorPath); err != nil {
					t.Fatal("failed cleanup removed the old collector configuration")
				}
				runner.failCleanup = false
				err = manager.Up(context.Background(), options)
			}
			if !errors.Is(err, errBundlePull) {
				t.Fatalf("Up = %v, want pull failure after retiring telemetry", err)
			}
			pending, err := loadMetadata(manager.metadataPath())
			if err != nil || pending.Profile != "observability" || !pending.CreatedAt.Equal(createdAt) {
				t.Fatalf("failed startup changed applied metadata: %+v, %v", pending, err)
			}
			runner.failPull = false
			if err := manager.Up(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			wantCleanupCalls := 1
			if failCleanup {
				wantCleanupCalls++
			}
			if runner.cleanupCalls != wantCleanupCalls || runner.activeTelemetry {
				t.Fatalf("cleanup calls=%d, active telemetry=%t", runner.cleanupCalls, runner.activeTelemetry)
			}
			metadata, err := loadMetadata(manager.metadataPath())
			if err != nil || metadata.Profile != "" || metadata.ComposeProject != ProjectName || !metadata.CreatedAt.Equal(createdAt) || !slices.Equal(metadata.ExternalInsecureRegistries, oldMetadata.ExternalInsecureRegistries) {
				t.Fatalf("reconciled metadata = %+v, %v", metadata, err)
			}
			if _, err := os.Stat(collectorPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("retired collector configuration remains: %v", err)
			}
			if err := manager.Down(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := manager.Up(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			for path, want := range retained {
				if got := mustReadTelemetryFile(t, path); !bytes.Equal(got, want) {
					t.Fatalf("retained file changed: %s", path)
				}
			}
			var contexts struct {
				Current string                     `json:"current_context"`
				Values  map[string]json.RawMessage `json:"contexts"`
			}
			if err := json.Unmarshal(mustReadTelemetryFile(t, manager.ConfigPath), &contexts); err != nil || contexts.Current != "remote" || !bytes.Contains(contexts.Values["remote"], []byte("remote.example:443")) || contexts.Values[ContextName] == nil {
				t.Fatalf("user context was not preserved: %+v, %v", contexts, err)
			}
			for _, name := range []string{"POSTGRES_PORT", "GATEWAY_CONTROL_PORT", "GATEWAY_HTTP_PORT", "GATEWAY_SSH_PORT"} {
				if value, err := readMaterializedEnvValue(manager.envPath(), name); err != nil || value == "" {
					t.Fatalf("managed port %s was lost: %v", name, err)
				}
			}
		})
	}
}

var errTelemetryCleanup = errors.New("test telemetry cleanup failed")

type telemetryMigrationRunner struct {
	bundleRetryRunner
	manager         *Manager
	oldCompose      []byte
	activeTelemetry bool
	failCleanup     bool
	cleanupCalls    int
	stopped         bool
}

func (r *telemetryMigrationRunner) Run(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	if slices.Contains(args, "--volumes") || slices.Contains(args, "volume") {
		return fmt.Errorf("unexpected storage cleanup: %v", args)
	}
	if slices.Contains(args, "otel-collector") || slices.Contains(args, "otel-lgtm") {
		want := r.manager.composeArgs("observability", "rm", "-sf", "otel-collector", "otel-lgtm")
		if !slices.Equal(args, want) {
			return fmt.Errorf("telemetry cleanup touched unexpected services: %v", args)
		}
		current, err := os.ReadFile(r.manager.composePath())
		if err != nil || !bytes.Equal(current, r.oldCompose) {
			return fmt.Errorf("old Compose was overwritten before cleanup: %v", err)
		}
		r.cleanupCalls++
		if r.failCleanup {
			return errTelemetryCleanup
		}
		r.activeTelemetry = false
	}
	if slices.Contains(args, "down") {
		r.stopped = true
	} else if slices.Contains(args, "up") {
		r.stopped = false
	}
	return r.bundleRetryRunner.Run(ctx, stdout, stderr, name, args...)
}

func (r *telemetryMigrationRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.stopped && slices.Contains(args, "ps") {
		return []byte("[]"), nil
	}
	return r.bundleRetryRunner.Output(ctx, name, args...)
}

func mustReadTelemetryFile(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestExternalTelemetryConfiguration(t *testing.T) {
	t.Setenv(localDNSNameserversEnv, "192.0.2.53")
	t.Setenv("AXERN_OTEL_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
	if _, err := localTelemetryEnv(); err == nil {
		t.Fatal("enabled telemetry accepted an implicit backend")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://telemetry.example:4317")
	values, err := localTelemetryEnv()
	if err != nil || values["OTEL_EXPORTER_OTLP_ENDPOINT"] != "https://telemetry.example:4317" || values["OTEL_EXPORTER_OTLP_INSECURE"] != "false" {
		t.Fatalf("external telemetry settings = %v, %v", values, err)
	}
	manager := &Manager{Dir: t.TempDir()}
	if err := manager.writeEnv(nil); err != nil {
		t.Fatal(err)
	}
	if manager.localTelemetryConfigurationChanged() {
		t.Fatal("current telemetry settings require reconfiguration")
	}
	t.Setenv("AXERN_OTEL_ENABLED", "false")
	if !manager.localTelemetryConfigurationChanged() {
		t.Fatal("disabling export would bypass reconciliation")
	}
	t.Setenv("AXERN_OTEL_ENABLED", "invalid")
	if _, err := localTelemetryEnv(); err == nil || strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid config should identify the variable without its value: %v", err)
	}
}
