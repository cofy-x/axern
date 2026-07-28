package computeruse

import (
	"os"
	"os/exec"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/provider"
)

const ProviderName = provider.CapabilityComputerUse

func DetectFromEnv() provider.Provider {
	dependencies := providerDependencies()
	if unavailable := firstUnavailableDependency(dependencies); unavailable != nil {
		return provider.Unavailable(ProviderName, unavailable.Name+" unavailable: "+unavailable.Reason, provider.CapabilityComputerUse).
			WithBackend("x11").
			WithDependencies(dependencies...)
	}
	return provider.Static(ProviderName, provider.CapabilityComputerUse).
		WithBackend("x11").
		WithDependencies(dependencies...)
}

func providerDependencies() []provider.Dependency {
	display := strings.TrimSpace(os.Getenv("DISPLAY"))
	return []provider.Dependency{
		{Name: "display_env", Available: display != "", Reason: unavailableReason(display != "", "DISPLAY is not set")},
		providerDependency(screenshotBackendDependency()),
		providerDependency(displayBackendDependency()),
		providerDependency(inputBackendDependency()),
	}
}

func providerDependency(dep DependencyStatus) provider.Dependency {
	return provider.Dependency{Name: dep.Name, Available: dep.Available, Reason: dep.Reason}
}

func firstUnavailableDependency(dependencies []provider.Dependency) *provider.Dependency {
	for i := range dependencies {
		if !dependencies[i].Available {
			return &dependencies[i]
		}
	}
	return nil
}

func unavailableReason(available bool, reason string) string {
	if available {
		return ""
	}
	return reason
}

func commandBackendDependency(name string, overrideEnv string, binary string) DependencyStatus {
	if strings.TrimSpace(os.Getenv(overrideEnv)) != "" {
		return DependencyStatus{Name: name, Available: true}
	}
	if _, err := exec.LookPath(binary); err != nil {
		return DependencyStatus{Name: name, Available: false, Reason: binary + " is not installed"}
	}
	return DependencyStatus{Name: name, Available: true}
}

func screenshotBackendDependency() DependencyStatus {
	return commandBackendDependency("screenshot_backend", "AXERN_SANDBOXD_SCREENSHOT_CMD", "import")
}

func displayBackendDependency() DependencyStatus {
	return commandBackendDependency("display_backend", "AXERN_SANDBOXD_DISPLAY_CMD", "xdotool")
}

func inputBackendDependency() DependencyStatus {
	if strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_MOUSE_CMD")) != "" && strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_KEYBOARD_CMD")) != "" {
		return DependencyStatus{Name: "input_backend", Available: true}
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		return DependencyStatus{Name: "input_backend", Available: false, Reason: "xdotool is not installed"}
	}
	return DependencyStatus{Name: "input_backend", Available: true}
}
