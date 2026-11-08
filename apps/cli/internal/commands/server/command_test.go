package server

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
)

func TestServerSurfaceHasNoResetOrBackendChoice(t *testing.T) {
	cmd := Command(command.Runtime{})
	for _, name := range []string{"start", "stop", "status", "logs", "renew-services"} {
		found, _, err := cmd.Find([]string{name})
		if err != nil || found == cmd {
			t.Fatalf("server %s missing: %v", name, err)
		}
	}
	for _, name := range []string{"reset", "up", "down", "docker", "lima"} {
		found, _, _ := cmd.Find([]string{name})
		if found != cmd {
			t.Fatalf("unexpected server operation %s", name)
		}
	}
}

func TestServerRejectsMisleadingJSONOutput(t *testing.T) {
	cmd := Command(command.Runtime{Options: &command.Options{Output: "json"}})
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--output must be table") {
		t.Fatalf("JSON output accepted: %v", err)
	}
}
