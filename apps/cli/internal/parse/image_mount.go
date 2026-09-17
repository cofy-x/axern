package parse

import (
	"fmt"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func ImageMounts(values []string) ([]*commonv1.ImageMount, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*commonv1.ImageMount, 0, len(values))
	for _, value := range values {
		image, targetAndOptions, ok := strings.Cut(value, ":/")
		if !ok || strings.TrimSpace(image) == "" || strings.TrimSpace(targetAndOptions) == "" {
			return nil, fmt.Errorf("invalid image mount %q, want image:/container/path", value)
		}
		target := "/" + targetAndOptions
		if strings.Contains(target, ":") {
			return nil, fmt.Errorf("invalid image mount %q, image mounts are always read-only and accept no options", value)
		}
		out = append(out, &commonv1.ImageMount{
			Image:  strings.TrimSpace(image),
			Target: strings.TrimSpace(target),
		})
	}
	return out, nil
}
