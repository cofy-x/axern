package runkernel

import (
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/protobuf/proto"
)

func ConfigOrEmpty(config *commonv1.ExecutionConfig) *commonv1.ExecutionConfig {
	if config == nil {
		return &commonv1.ExecutionConfig{}
	}
	return config
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func CloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func CloneConfig(in *commonv1.ExecutionConfig) *commonv1.ExecutionConfig {
	if in == nil {
		return &commonv1.ExecutionConfig{}
	}
	return proto.Clone(in).(*commonv1.ExecutionConfig)
}

func IsTerminal(status runv1.RunStatus) bool {
	switch status {
	case runv1.RunStatus_RUN_STATUS_SUCCEEDED, runv1.RunStatus_RUN_STATUS_FAILED, runv1.RunStatus_RUN_STATUS_CANCELLED:
		return true
	default:
		return false
	}
}
