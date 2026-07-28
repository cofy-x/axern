package image

import (
	"os"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestResolveImagemgrSocketPathPreservesExplicitPath(t *testing.T) {
	got := resolveImagemgrSocketPath("/tmp/custom-imagemgr.sock")
	if got != "/tmp/custom-imagemgr.sock" {
		t.Fatalf("resolveImagemgrSocketPath() = %q, want explicit path", got)
	}
}

func TestResolveImagemgrSocketPathPreservesDefaultWhenFallbackMissing(t *testing.T) {
	got := resolveImagemgrSocketPath(config.DefaultImageManagerSocket)
	if got != config.DefaultImageManagerSocket {
		t.Fatalf("resolveImagemgrSocketPath() = %q, want default path", got)
	}
}

func TestResolveImagemgrSocketPathPreservesExistingDefault(t *testing.T) {
	if _, err := os.Stat(config.DefaultImageManagerSocket); err == nil {
		got := resolveImagemgrSocketPath(config.DefaultImageManagerSocket)
		if got != config.DefaultImageManagerSocket {
			t.Fatalf("resolveImagemgrSocketPath() = %q, want existing default path", got)
		}
	}
}
