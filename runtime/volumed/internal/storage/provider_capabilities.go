package storage

import (
	"fmt"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

func validateProviderCapabilities(provider Provider) error {
	capabilities := provider.Capabilities()
	if capabilities.Backend == storagev1.VolumeBackend_VOLUME_BACKEND_UNSPECIFIED {
		return fmt.Errorf("volume provider %s capabilities backend is required", provider.Backend())
	}
	if capabilities.Backend != provider.Backend() {
		return fmt.Errorf("volume provider %s capabilities backend %s does not match provider backend", provider.Backend(), capabilities.Backend)
	}
	if len(capabilities.AccessModes) == 0 {
		return fmt.Errorf("volume provider %s must declare at least one access mode", provider.Backend())
	}
	for _, mode := range capabilities.AccessModes {
		if mode == storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_UNSPECIFIED {
			return fmt.Errorf("volume provider %s access mode is unspecified", provider.Backend())
		}
	}
	if len(capabilities.ConsistencyProfiles) == 0 {
		return fmt.Errorf("volume provider %s must declare at least one consistency profile", provider.Backend())
	}
	for _, profile := range capabilities.ConsistencyProfiles {
		if profile == storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_UNSPECIFIED {
			return fmt.Errorf("volume provider %s consistency profile is unspecified", provider.Backend())
		}
	}
	if capabilities.RuntimeCompatibility == nil || (!capabilities.RuntimeCompatibility.GetSupportsRunc() && !capabilities.RuntimeCompatibility.GetSupportsRunsc()) {
		return fmt.Errorf("volume provider %s must declare runtime compatibility", provider.Backend())
	}
	return nil
}

func providerSupportsAccessMode(capabilities ProviderCapabilities, mode storagev1.VolumeAccessMode) bool {
	for _, supported := range capabilities.AccessModes {
		if supported == mode {
			return true
		}
	}
	return false
}

func providerSupportsConsistencyProfile(capabilities ProviderCapabilities, profile storagev1.VolumeConsistencyProfile) bool {
	for _, supported := range capabilities.ConsistencyProfiles {
		if supported == profile {
			return true
		}
	}
	return false
}
