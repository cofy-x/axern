package verifyutil

import (
	"fmt"
	"path/filepath"
	"strings"

	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

type StringSliceFlag []string

func (s *StringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *StringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func ParseUserEnvs(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid user env %q", value)
		}
		out[key] = val
	}
	return out, nil
}

func ParseMounts(values []string) ([]*privatenodev1.SandboxMount, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*privatenodev1.SandboxMount, 0, len(values))
	for _, value := range values {
		source, rest, ok := strings.Cut(value, ":")
		if !ok {
			return nil, fmt.Errorf("invalid mount %q", value)
		}
		target, optionsRaw, _ := strings.Cut(rest, ":")
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			return nil, fmt.Errorf("invalid mount %q", value)
		}
		mount := &privatenodev1.SandboxMount{
			Type:   "bind",
			Source: filepath.Clean(source),
			Target: target,
		}
		if optionsRaw != "" {
			for _, option := range strings.Split(optionsRaw, ",") {
				option = strings.TrimSpace(option)
				if option != "" {
					mount.Options = append(mount.Options, option)
				}
			}
		}
		out = append(out, mount)
	}
	return out, nil
}
