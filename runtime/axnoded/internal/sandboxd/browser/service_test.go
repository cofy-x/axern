package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func TestOpenOwnsBrowserProcessAfterRequestContextCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "browser")
	markerPath := filepath.Join(dir, "started")
	script := "#!/bin/sh\nprintf started > \"$AXERN_BROWSER_MARKER\"\nsleep 5\n"
	if err := os.WriteFile(commandPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AXERN_SANDBOXD_BROWSER_CMD", commandPath)
	t.Setenv("AXERN_BROWSER_MARKER", markerPath)

	svc := NewService(DetectFromEnv(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	status, err := svc.Open(ctx, OpenRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !status.Running || status.Pid == 0 {
		t.Fatalf("open status = %#v", status)
	}
	if status.StartedAt == nil || status.LastActionAt == nil {
		t.Fatalf("open status missing session timestamps: %#v", status)
	}
	cancel()
	waitForFile(t, markerPath)
	time.Sleep(100 * time.Millisecond)
	status = svc.Status()
	if !status.Running {
		t.Fatalf("browser process should outlive request context cancellation: %#v", status)
	}
	if _, err := svc.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestHookUsesSharedWaiter(t *testing.T) {
	dir := t.TempDir()
	openPath := filepath.Join(dir, "open")
	closePath := filepath.Join(dir, "close")
	t.Setenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD", "printf '%s' \"$AXERN_BROWSER_URL\" >"+openPath)
	t.Setenv("AXERN_SANDBOXD_BROWSER_CLOSE_CMD", "printf closed >"+closePath)

	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	svc := NewService(DetectFromEnv(), waiter)
	status, err := svc.Open(context.Background(), OpenRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !status.Running {
		t.Fatalf("open status = %#v", status)
	}
	if got := string(mustReadFile(t, openPath)); got != "https://example.com" {
		t.Fatalf("open hook = %q", got)
	}
	status, err = svc.Close(context.Background())
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if status.Running {
		t.Fatalf("close status = %#v", status)
	}
	if got := string(mustReadFile(t, closePath)); got != "closed" {
		t.Fatalf("close hook = %q", got)
	}
}

func TestOpenSameURLRefreshesSessionAction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "browser")
	markerPath := filepath.Join(dir, "started")
	script := "#!/bin/sh\nprintf started > \"$AXERN_BROWSER_MARKER\"\nsleep 5\n"
	if err := os.WriteFile(commandPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AXERN_SANDBOXD_BROWSER_CMD", commandPath)
	t.Setenv("AXERN_BROWSER_MARKER", markerPath)

	svc := NewService(DetectFromEnv(), nil)
	first, err := svc.Open(context.Background(), OpenRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	waitForFile(t, markerPath)
	time.Sleep(10 * time.Millisecond)
	second, err := svc.Open(context.Background(), OpenRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if first.LastActionAt == nil || second.LastActionAt == nil || !second.LastActionAt.After(*first.LastActionAt) {
		t.Fatalf("last action did not advance: first=%#v second=%#v", first, second)
	}
	if second.LastError != "" {
		t.Fatalf("last error = %q, want cleared on successful action", second.LastError)
	}
	if _, err := svc.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestDesktopOperationsUseCurrentBrowserSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell scripts")
	}
	dir := t.TempDir()
	openPath := filepath.Join(dir, "open")
	xdotoolLog := filepath.Join(dir, "xdotool.log")
	xdotoolPath := filepath.Join(dir, "xdotool")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$AXERN_XDOTOOL_LOG\"\n"
	if err := os.WriteFile(xdotoolPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("AXERN_XDOTOOL_LOG", xdotoolLog)
	t.Setenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD", "printf '%s' \"$AXERN_BROWSER_URL\" >"+openPath)

	svc := NewService(DetectFromEnv(), nil)
	status, err := svc.Navigate(context.Background(), NavigateRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if !status.Running || status.URL != "https://example.com" {
		t.Fatalf("navigate status = %#v", status)
	}
	if status.StartedAt == nil || status.LastActionAt == nil {
		t.Fatalf("navigate status missing session timestamps: %#v", status)
	}
	if got := string(mustReadFile(t, openPath)); got != "https://example.com" {
		t.Fatalf("navigate hook = %q", got)
	}
	if _, err := svc.Resize(context.Background(), ResizeRequest{Width: 1024, Height: 768}); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	if _, err := svc.Click(context.Background(), ClickRequest{X: 10, Y: 20}); err != nil {
		t.Fatalf("Click() error = %v", err)
	}
	if _, err := svc.Type(context.Background(), TypeRequest{Text: "hello", DelayMS: 1}); err != nil {
		t.Fatalf("Type() error = %v", err)
	}
	if _, err := svc.Wait(context.Background(), WaitRequest{TimeoutMS: 1}); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	log := string(mustReadFile(t, xdotoolLog))
	for _, want := range []string{
		"getactivewindow windowsize 1024 768",
		"mousemove 10 20 click 1",
		"type --delay 1 -- hello",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("xdotool log missing %q in %q", want, log)
		}
	}
}

func TestOpenNavigatesExistingBrowserSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "browser")
	firstStartedPath := filepath.Join(dir, "started")
	logPath := filepath.Join(dir, "browser.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$AXERN_BROWSER_LOG"
if [ ! -f "$AXERN_BROWSER_STARTED" ]; then
  printf started > "$AXERN_BROWSER_STARTED"
  sleep 5
fi
`
	if err := os.WriteFile(commandPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AXERN_SANDBOXD_BROWSER_CMD", commandPath)
	t.Setenv("AXERN_BROWSER_STARTED", firstStartedPath)
	t.Setenv("AXERN_BROWSER_LOG", logPath)

	svc := NewService(DetectFromEnv(), nil)
	status, err := svc.Open(context.Background(), OpenRequest{URL: "https://one.example"})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !status.Running || status.URL != "https://one.example" {
		t.Fatalf("first open status = %#v", status)
	}
	waitForFile(t, firstStartedPath)

	status, err = svc.Open(context.Background(), OpenRequest{URL: "https://two.example"})
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if !status.Running || status.URL != "https://two.example" {
		t.Fatalf("second open status = %#v", status)
	}
	log := string(mustReadFile(t, logPath))
	if !strings.Contains(log, "https://one.example") || !strings.Contains(log, "https://two.example") {
		t.Fatalf("browser command log = %q", log)
	}
	if _, err := svc.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
