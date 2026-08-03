package local

import (
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
)

func TestLocalLifecycleSurface(t *testing.T) {
	cmd := Command(command.Runtime{}, "1.2.3")
	for _, name := range []string{"up", "status", "logs", "doctor", "down", "reset", "upgrade", "path"} {
		found, _, err := cmd.Find([]string{name})
		if err != nil || found == cmd {
			t.Fatalf("local subcommand %q is missing: %v", name, err)
		}
	}
}
