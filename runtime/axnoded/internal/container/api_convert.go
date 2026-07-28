package container

import (
	"strconv"
	"strings"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

var timestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05 -0700 MST",
}

func ParseTimestamp(timestamp string) int64 {
	timestamp = strings.TrimSpace(timestamp)
	if timestamp == "" {
		return 0
	}

	normalized := timestamp
	if prefix, _, ok := strings.Cut(normalized, " m="); ok {
		normalized = prefix
	}

	for _, layout := range timestampLayouts {
		if rt, err := time.Parse(layout, normalized); err == nil {
			return rt.Unix()
		}
	}

	t, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return 0
	}
	return t
}

func MountsToAPI(mounts []spec.Mount) []*runtime.Mount {
	out := make([]*runtime.Mount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, &runtime.Mount{
			Source:  mount.Source,
			Target:  mount.Destination,
			Type:    mount.Type,
			Options: mount.Options,
		})
	}
	return out
}
