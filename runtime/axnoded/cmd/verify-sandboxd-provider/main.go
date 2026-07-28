package main

import (
	"flag"
	"strings"
)

type config struct {
	mode            string
	socketPath      string
	mousePath       string
	keyboardPath    string
	browserOpenPath string
}

type httpResult struct {
	status int
	body   []byte
}

func main() {
	cfg := parseFlags()
	client := unixClient(cfg.socketPath)
	switch cfg.mode {
	case "baseline":
		checkBaseline(client)
	case "optional":
		checkOptional(client, cfg.mousePath, cfg.keyboardPath, cfg.browserOpenPath)
	default:
		fail("unsupported mode %q", cfg.mode)
	}
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.mode, "mode", "", "provider conformance mode: baseline or optional")
	flag.StringVar(&cfg.socketPath, "socket", "", "sandboxd Unix socket path")
	flag.StringVar(&cfg.mousePath, "mouse-file", "", "expected computer-use mouse command output path")
	flag.StringVar(&cfg.keyboardPath, "keyboard-file", "", "expected computer-use keyboard command output path")
	flag.StringVar(&cfg.browserOpenPath, "browser-open-file", "", "expected browser open command output path")
	flag.Parse()
	if cfg.mode == "" {
		fail("--mode is required")
	}
	if strings.TrimSpace(cfg.socketPath) == "" {
		fail("--socket is required")
	}
	return cfg
}
