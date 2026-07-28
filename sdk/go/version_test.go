package axernsdk

import "testing"

func TestPlatformName(t *testing.T) {
	if PlatformName() != "axern" {
		t.Fatalf("unexpected platform name: %q", PlatformName())
	}
}
