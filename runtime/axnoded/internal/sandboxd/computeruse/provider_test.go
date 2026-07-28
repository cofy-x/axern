package computeruse

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/provider"
)

func TestDetectFromEnvRequiresDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	detected := DetectFromEnv()
	if detected.Available {
		t.Fatalf("expected provider to require display: %#v", detected)
	}
}

func TestDetectFromEnvAvailable(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("AXERN_SANDBOXD_SCREENSHOT_CMD", "screenshot")
	t.Setenv("AXERN_SANDBOXD_DISPLAY_CMD", "display")
	t.Setenv("AXERN_SANDBOXD_MOUSE_CMD", "mouse")
	t.Setenv("AXERN_SANDBOXD_KEYBOARD_CMD", "keyboard")

	detected := DetectFromEnv()
	if !detected.Available {
		t.Fatalf("expected provider to be available: %#v", detected)
	}
	if len(detected.Capabilities) != 1 || detected.Capabilities[0] != provider.CapabilityComputerUse {
		t.Fatalf("capabilities = %#v", detected.Capabilities)
	}
	if detected.Backend != "x11" {
		t.Fatalf("backend = %q, want x11", detected.Backend)
	}
	if len(detected.Dependencies) == 0 {
		t.Fatalf("expected dependencies: %#v", detected)
	}
}

func TestDetectFromEnvRequiresBackends(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("PATH", t.TempDir())

	detected := DetectFromEnv()
	if detected.Available {
		t.Fatalf("expected provider to require command backends: %#v", detected)
	}
	if detected.Reason == "" {
		t.Fatalf("expected unavailable reason: %#v", detected)
	}
	if len(detected.Dependencies) == 0 {
		t.Fatalf("expected dependency diagnostics: %#v", detected)
	}
}
