package browser

import (
	"os"
	"os/exec"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/provider"
)

const ProviderName = provider.CapabilityBrowser

func DetectFromEnv() provider.Provider {
	command, reason := ResolveCommand()
	if command == "" {
		return provider.Unavailable(ProviderName, reason, provider.CapabilityBrowser).
			WithDependencies(provider.Dependency{Name: "browser_command", Available: false, Reason: reason})
	}
	return provider.Static(ProviderName, provider.CapabilityBrowser).
		WithBackend("desktop").
		WithCommand(command).
		WithDependencies(provider.Dependency{Name: "browser_command", Available: true})
}

func ResolveCommand() (string, string) {
	if command := strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_BROWSER_CMD")); command != "" {
		if _, err := exec.LookPath(command); err != nil {
			return "", command + " is not installed"
		}
		return command, ""
	}
	if strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD")) != "" {
		return "/bin/sh", ""
	}
	for _, command := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "firefox"} {
		if _, err := exec.LookPath(command); err == nil {
			return command, ""
		}
	}
	return "", "no supported browser command found"
}
