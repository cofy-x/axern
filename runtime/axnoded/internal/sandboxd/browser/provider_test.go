package browser

import "testing"

func TestDetectFromEnvUnavailableWithoutBrowserCommand(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("AXERN_SANDBOXD_BROWSER_CMD", "")
	t.Setenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD", "")

	detected := DetectFromEnv()
	if detected.Available {
		t.Fatalf("expected provider unavailable without browser command: %#v", detected)
	}
}

func TestDetectFromEnvAvailableWithHook(t *testing.T) {
	t.Setenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD", "true")

	detected := DetectFromEnv()
	if !detected.Available {
		t.Fatalf("expected provider available with hook: %#v", detected)
	}
	if detected.Backend != "desktop" || detected.Command == "" {
		t.Fatalf("provider metadata = %#v", detected)
	}
}

func TestValidateURL(t *testing.T) {
	for _, value := range []string{"about:blank", "https://example.com", "http://127.0.0.1", "file:///tmp/index.html"} {
		if err := validateURL(value); err != nil {
			t.Fatalf("validateURL(%q) error = %v", value, err)
		}
	}
	if err := validateURL("javascript:alert(1)"); err == nil {
		t.Fatalf("expected unsupported scheme")
	}
}

func TestBrowserArgs(t *testing.T) {
	firefoxArgs := browserArgs("/usr/bin/firefox", "https://example.com")
	if len(firefoxArgs) != 2 || firefoxArgs[0] != "-new-window" || firefoxArgs[1] != "https://example.com" {
		t.Fatalf("firefox args = %#v", firefoxArgs)
	}
	chromiumArgs := browserArgs("chromium", "https://example.com")
	if len(chromiumArgs) == 0 || chromiumArgs[len(chromiumArgs)-1] != "https://example.com" {
		t.Fatalf("chromium args = %#v", chromiumArgs)
	}
}
