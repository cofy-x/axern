package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestParseTerminalClientMessage(t *testing.T) {
	t.Parallel()
	msg, ok := parseTerminalClientMessage([]byte(`{"type":"stdin","data":"echo ok\n"}`))
	if !ok {
		t.Fatal("parseTerminalClientMessage() ok = false")
	}
	if msg.Type != "stdin" || string(msg.Data) != "echo ok\n" {
		t.Fatalf("message = %#v", msg)
	}

	ping, ok := parseTerminalClientMessage([]byte(`{"type":"ping"}`))
	if !ok || ping.Type != "ping" {
		t.Fatalf("ping = %#v ok=%v", ping, ok)
	}
}

func TestParseTerminalClientMessageRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	if _, ok := parseTerminalClientMessage([]byte(`not-json`)); ok {
		t.Fatal("parseTerminalClientMessage() ok = true, want false")
	}
}

func TestTerminalOpenOptionsFromRequest(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/terminal/allocation/alloc-1?shell=/bin/bash", nil)
	opts := terminalOpenOptionsFromRequest(req)
	if len(opts.Argv) != 1 || opts.Argv[0] != "/bin/bash" {
		t.Fatalf("argv = %#v, want /bin/bash", opts.Argv)
	}
	if !opts.TTY {
		t.Fatalf("TTY = false, want true")
	}
	if opts.Env["TERM"] != "xterm-256color" {
		t.Fatalf("env = %#v", opts.Env)
	}
}

func TestTerminalOpenOptionsFromRequestRejectsUnsupportedShell(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/terminal/allocation/alloc-1?shell=/bin/python", nil)
	opts := terminalOpenOptionsFromRequest(req)
	if len(opts.Argv) != 0 {
		t.Fatalf("argv = %#v, want default shell", opts.Argv)
	}
	if !opts.TTY {
		t.Fatalf("TTY = false, want true")
	}
	if opts.Env["TERM"] != "xterm-256color" {
		t.Fatalf("env = %#v", opts.Env)
	}
}
