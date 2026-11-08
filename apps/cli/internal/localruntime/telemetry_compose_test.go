package localruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// This opt-in integration check uses an explicitly supplied local fixture image.
// It creates its own Compose project and volume; it never starts Axern services
// or touches the CLI-owned development project or registry network.
func TestRetireBundledTelemetryCompose(t *testing.T) {
	image := os.Getenv("AXERN_COMPOSE_MIGRATION_TEST_IMAGE")
	if image == "" {
		t.Skip("set AXERN_COMPOSE_MIGRATION_TEST_IMAGE to a local image with /bin/sh and sleep")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	manager := &Manager{Dir: t.TempDir(), Runner: ExecRunner{}, Stdout: io.Discard, Stderr: io.Discard}
	project := fmt.Sprintf("axern-telemetry-migration-%d", time.Now().UnixNano())
	if err := saveMetadata(manager.metadataPath(), Metadata{Version: "dev", ComposeProject: project, Profile: "observability"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.envPath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	oldCompose := fmt.Sprintf(`services:
  postgres:
    image: %q
    pull_policy: never
    entrypoint: ["/bin/sh", "-c", "exec sleep 300"]
    volumes: ["retained:/retained"]
  node:
    image: %q
    pull_policy: never
    entrypoint: ["/bin/sh", "-c", "exec sleep 300"]
  registry:
    image: %q
    pull_policy: never
    entrypoint: ["/bin/sh", "-c", "exec sleep 300"]
`, image, image, image)
	telemetry := fmt.Sprintf(`  otel-collector:
    image: %q
    pull_policy: never
    profiles: [observability]
    entrypoint: ["/bin/sh", "-c", "exec sleep 300"]
  otel-lgtm:
    image: %q
    pull_policy: never
    profiles: [observability]
    entrypoint: ["/bin/sh", "-c", "exec sleep 300"]
`, image, image)
	coreCompose := oldCompose + "volumes:\n  retained:\n"
	if err := os.WriteFile(manager.composePath(), []byte(oldCompose+telemetry+"volumes:\n  retained:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := manager.composeRun(cleanup, "observability", "down", "--remove-orphans", "--volumes", "--timeout", "1"); err != nil {
			t.Errorf("remove isolated telemetry migration fixture: %v", err)
		}
	})
	if err := manager.composeRun(ctx, "observability", "up", "-d", "--wait", "--wait-timeout", "20"); err != nil {
		t.Fatal(err)
	}
	if err := manager.composeRun(ctx, "observability", "exec", "-T", "postgres", "/bin/sh", "-c", "printf retained > /retained/marker"); err != nil {
		t.Fatal(err)
	}
	before := telemetryFixtureContainers(t, ctx, manager)
	if len(before) != 5 {
		t.Fatalf("fixture containers = %v, want three core and two telemetry", before)
	}
	if err := manager.retireBundledTelemetry(ctx); err != nil {
		t.Fatal(err)
	}
	after := telemetryFixtureContainers(t, ctx, manager)
	if len(after) != 3 {
		t.Fatalf("containers after retirement = %v, want only the core", after)
	}
	for _, service := range []string{"postgres", "node", "registry"} {
		if after[service] == "" || after[service] != before[service] {
			t.Fatalf("retirement restarted or removed %s: before=%v after=%v", service, before, after)
		}
	}
	if err := os.WriteFile(manager.composePath(), []byte(coreCompose), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveMetadata(manager.metadataPath(), Metadata{Version: "dev", ComposeProject: project}); err != nil {
		t.Fatal(err)
	}
	if err := manager.composeRun(ctx, "", "down", "--remove-orphans", "--timeout", "1"); err != nil {
		t.Fatal(err)
	}
	if err := manager.composeRun(ctx, "", "up", "-d", "--wait", "--wait-timeout", "20"); err != nil {
		t.Fatal(err)
	}
	marker, err := manager.composeOutput(ctx, "", "exec", "-T", "postgres", "cat", "/retained/marker")
	if err != nil || string(marker) != "retained" {
		t.Fatalf("retained volume marker = %q, %v", marker, err)
	}
	if containers := telemetryFixtureContainers(t, ctx, manager); len(containers) != 3 || containers["otel-collector"] != "" || containers["otel-lgtm"] != "" {
		t.Fatalf("core restart revived telemetry: %v", containers)
	}
}

func telemetryFixtureContainers(t *testing.T, ctx context.Context, manager *Manager) map[string]string {
	t.Helper()
	data, err := manager.composeOutput(ctx, "observability", "ps", "-a", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		ID      string
		Service string
		State   string
	}
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		for decoder.More() {
			var container row
			if err := decoder.Decode(&container); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, container)
		}
	}
	containers := make(map[string]string)
	for _, container := range rows {
		if container.State != "running" || container.ID == "" {
			t.Fatalf("fixture service is not running: %+v", container)
		}
		containers[container.Service] = container.ID
	}
	return containers
}
