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
			return nil, fmt.Errorf("invalid image mount %q, want image:/container/path[:ro]", value)
		}
		target := "/" + targetAndOptions
		options := ""
		if idx := strings.LastIndex(target, ":"); idx >= 0 {
			options = strings.TrimSpace(target[idx+1:])
			target = target[:idx]
		}
		if options != "" && options != "ro" {
			return nil, fmt.Errorf("invalid image mount %q, only ro option is supported", value)
		}
		out = append(out, &commonv1.ImageMount{
			Image:    strings.TrimSpace(image),
			Target:   strings.TrimSpace(target),
			Readonly: true,
		})
	}
	return out, nil
}
