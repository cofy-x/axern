package sandboxaccess

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func TestSandboxdProviderFailureDetailIncludesDependencies(t *testing.T) {
	detail := SandboxdProviderFailureDetail(wire.CapabilityProvider{
		Name:      "browser",
		State:     "unavailable",
		Available: false,
		Reason:    "browser command unavailable",
		Dependencies: []wire.ProviderDependency{
			{Name: "chromium", Available: false, Reason: "not found"},
			{Name: "display", Available: true},
		},
	})

	for _, want := range []string{
		"browser provider unavailable",
		"browser command unavailable",
		"missing dependencies: chromium (not found)",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("SandboxdProviderFailureDetail() = %q, want %q", detail, want)
		}
	}
}
