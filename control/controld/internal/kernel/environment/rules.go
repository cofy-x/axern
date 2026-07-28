package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

const DefaultNamespace = "default"

func NormalizeNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return DefaultNamespace
	}
	return namespace
}

func SpecHash(spec *environmentv1.EnvironmentSpec, template *catalogv1.RuntimeTemplate) string {
	payload, _ := json.Marshal(struct {
		Spec     *environmentv1.EnvironmentSpec `json:"spec"`
		Template *catalogv1.RuntimeTemplate     `json:"template"`
	}{
		Spec:     spec,
		Template: template,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func MatchFilter(env *environmentv1.Environment, filter *environmentv1.ListFilter) bool {
	if filter == nil {
		return true
	}
	if ns := strings.TrimSpace(filter.GetNamespace()); ns != "" && env.GetNamespace() != ns {
		return false
	}
	if len(filter.GetStatuses()) > 0 {
		ok := false
		for _, status := range filter.GetStatuses() {
			if env.GetStatus() == status {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return labelsMatch(env.GetLabels(), filter.GetLabels())
}

func ParseStatus(value string) environmentv1.EnvironmentStatus {
	if n, ok := environmentv1.EnvironmentStatus_value[value]; ok {
		return environmentv1.EnvironmentStatus(n)
	}
	return environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_UNSPECIFIED
}

func labelsMatch(have, want map[string]string) bool {
	for key, value := range want {
		if have[key] != value {
			return false
		}
	}
	return true
}
