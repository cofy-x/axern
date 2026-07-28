package computeruse

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func TestServiceScreenshotWithCommandOverride(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("AXERN_SANDBOXD_SCREENSHOT_CMD", "printf %s "+base64.StdEncoding.EncodeToString(tinyPNG())+" | base64 -d")
	t.Setenv("AXERN_SANDBOXD_DISPLAY_CMD", "printf '1280 720'")
	t.Setenv("AXERN_SANDBOXD_MOUSE_CMD", "true")
	t.Setenv("AXERN_SANDBOXD_KEYBOARD_CMD", "true")

	service := NewService(DetectFromEnv(), nil)
	screenshot, err := service.Screenshot(context.Background(), ScreenshotRequest{Format: "png", Quality: 80, Scale: 1})
	if err != nil {
		t.Fatalf("Screenshot() error = %v", err)
	}
	data := screenshot.Data
	if len(data) == 0 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("screenshot is not png: %q", data[:min(len(data), 8)])
	}
	display, err := service.Display(context.Background())
	if err != nil {
		t.Fatalf("Display() error = %v", err)
	}
	if display.Width != 1280 || display.Height != 720 {
		t.Fatalf("display = %#v", display)
	}

	status := service.Status(context.Background())
	if !status.Available {
		t.Fatalf("status = %#v", status)
	}
	for _, name := range []string{"display_env", "screenshot_backend", "display_backend", "display_server"} {
		dependency := findDependency(status.Dependencies, name)
		if dependency == nil || !dependency.Available {
			t.Fatalf("dependency %s = %#v in %#v", name, dependency, status.Dependencies)
		}
	}
}

func TestServiceMouseKeyboardWithCommandOverride(t *testing.T) {
	dir := t.TempDir()
	mousePath := filepath.Join(dir, "mouse")
	keyboardPath := filepath.Join(dir, "keyboard")
	t.Setenv("DISPLAY", ":99")
	t.Setenv("AXERN_SANDBOXD_SCREENSHOT_CMD", "true")
	t.Setenv("AXERN_SANDBOXD_DISPLAY_CMD", "printf '1280 720'")
	t.Setenv("AXERN_SANDBOXD_MOUSE_CMD", "printf '%s:%s:%s:%s' \"$AXERN_COMPUTER_USE_MOUSE_X\" \"$AXERN_COMPUTER_USE_MOUSE_Y\" \"$AXERN_COMPUTER_USE_MOUSE_BUTTON\" \"$AXERN_COMPUTER_USE_MOUSE_ACTION\" >"+mousePath)
	t.Setenv("AXERN_SANDBOXD_KEYBOARD_CMD", "printf '%s:%s' \"$AXERN_COMPUTER_USE_KEYBOARD_TEXT\" \"$AXERN_COMPUTER_USE_KEYBOARD_KEY\" >"+keyboardPath)

	service := NewService(DetectFromEnv(), nil)
	if err := service.Mouse(context.Background(), MouseRequest{X: 7, Y: 9, Button: "1"}); err != nil {
		t.Fatalf("Mouse() error = %v", err)
	}
	if err := service.Keyboard(context.Background(), KeyboardRequest{Text: "hello"}); err != nil {
		t.Fatalf("Keyboard() error = %v", err)
	}
	if got := string(mustRead(t, mousePath)); got != "7:9:1:click" {
		t.Fatalf("mouse command = %q", got)
	}
	if got := string(mustRead(t, keyboardPath)); got != "hello:" {
		t.Fatalf("keyboard command = %q", got)
	}
}

func TestServiceCommandOverrideWithSharedWaiter(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	dir := t.TempDir()
	mousePath := filepath.Join(dir, "mouse")
	t.Setenv("DISPLAY", ":99")
	t.Setenv("AXERN_SANDBOXD_SCREENSHOT_CMD", "true")
	t.Setenv("AXERN_SANDBOXD_DISPLAY_CMD", "printf '1280 720'")
	t.Setenv("AXERN_SANDBOXD_KEYBOARD_CMD", "true")
	t.Setenv("AXERN_SANDBOXD_MOUSE_CMD", "printf '%s:%s:%s:%s' \"$AXERN_COMPUTER_USE_MOUSE_X\" \"$AXERN_COMPUTER_USE_MOUSE_Y\" \"$AXERN_COMPUTER_USE_MOUSE_BUTTON\" \"$AXERN_COMPUTER_USE_MOUSE_ACTION\" >"+mousePath)

	service := NewService(DetectFromEnv(), waiter)
	if err := service.Mouse(context.Background(), MouseRequest{X: 11, Y: 13, Action: "move"}); err != nil {
		t.Fatalf("Mouse() error = %v", err)
	}
	if got := string(mustRead(t, mousePath)); got != "11:13:1:move" {
		t.Fatalf("mouse command = %q", got)
	}
}

func TestServiceCommandOverrideTimeout(t *testing.T) {
	originalWait := defaultCommandWait
	defaultCommandWait = 50 * time.Millisecond
	t.Cleanup(func() { defaultCommandWait = originalWait })
	t.Setenv("DISPLAY", ":99")
	t.Setenv("AXERN_SANDBOXD_SCREENSHOT_CMD", "sleep 5")
	t.Setenv("AXERN_SANDBOXD_DISPLAY_CMD", "printf '1280 720'")
	t.Setenv("AXERN_SANDBOXD_MOUSE_CMD", "true")
	t.Setenv("AXERN_SANDBOXD_KEYBOARD_CMD", "true")

	service := NewService(DetectFromEnv(), nil)
	_, err := service.Screenshot(context.Background(), ScreenshotRequest{})
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("Screenshot() error = %v, want ErrCommandFailed", err)
	}
}

func TestServiceRejectsUnsupportedShowCursorWithoutCommandOverride(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	fakeBackendCommands(t)

	service := NewService(DetectFromEnv(), nil)
	_, err := service.Screenshot(context.Background(), ScreenshotRequest{ShowCursor: true})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Screenshot() error = %v, want ErrInvalidArgument", err)
	}
}

func TestParseDisplayGeometryRejectsInvalidOutput(t *testing.T) {
	_, _, err := parseDisplayGeometry([]byte("wide tall"))
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("parseDisplayGeometry() error = %v, want ErrCommandFailed", err)
	}
}

func tinyPNG() []byte {
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	return data
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fakeBackendCommands(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"import", "xdotool"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func findDependency(items []DependencyStatus, name string) *DependencyStatus {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}
