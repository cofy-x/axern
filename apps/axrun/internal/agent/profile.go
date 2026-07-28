package agent

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/lib/go/agentprofile"
)

type Profile = agentprofile.Profile

// LoadProfile loads a provider-neutral LLM profile from the Axern config file.
// Empty profile names resolve through agent_profiles.current_profile and then
// "default".
func LoadProfile(configPath string, profileName string) (Profile, error) {
	name, profile, ok, err := agentprofile.Resolve(configPath, profileName)
	if err != nil {
		return Profile{}, err
	}
	if !ok {
		return Profile{}, fmt.Errorf("agent profile %q not found%s", name, ConfigPathHint(configPath))
	}
	return profile, nil
}

func ResolveProfile(inline map[string]Profile, configPath, profileName string) (Profile, error) {
	name := strings.TrimSpace(profileName)
	if profile, ok := inline[name]; ok {
		return profile, nil
	}
	return LoadProfile(configPath, profileName)
}

func ConfigPathHint(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return "; configure agent_profiles in AXERN_CONFIG or the default Axern user config"
	}
	return fmt.Sprintf(" in %s", configPath)
}
