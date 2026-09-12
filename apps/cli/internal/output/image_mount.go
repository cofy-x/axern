package output

import (
	"fmt"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func formatImageMounts(mounts []*commonv1.ImageMount) string {
	parts := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s:ro", mount.GetImage(), mount.GetTarget()))
	}
	return strings.Join(parts, " ")
}
