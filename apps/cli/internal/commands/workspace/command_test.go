package workspace

import (
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
)

func TestCreateRejectsMissingStorageBoundsBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{{"--node", "node-a", "--capacity", "513", "--inodes", "64"}, {"--node", "node-a", "--capacity", "1Gi"}, {"--node", "node-a", "--inodes", "64"}, {"--capacity", "1Gi", "--inodes", "64"}, {"--node", "node-a", "--capacity", "1Gi", "--inodes", "0"}} {
		cmd := createCommand(command.Runtime{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("incomplete storage request accepted: %v", args)
		}
	}
}
