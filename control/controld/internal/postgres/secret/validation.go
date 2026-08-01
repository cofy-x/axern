package pgsecret

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const maxSecretPayloadBytes = 64 << 10

func normalizeSecretData(secretType secretv1.SecretType, stringData map[string]string) (map[string]string, error) {
	if secretType == secretv1.SecretType_SECRET_TYPE_UNSPECIFIED {
		return nil, grpcstatus.Error(codes.InvalidArgument, "secret type is required")
	}
	data := map[string]string{}
	for key, value := range stringData {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, grpcstatus.Error(codes.InvalidArgument, "secret data keys must be non-empty")
		}
		data[key] = value
	}
	if len(data) == 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "string_data is required")
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, grpcstatus.Error(codes.InvalidArgument, "secret data must be valid UTF-8 strings")
	}
	if len(payload) > maxSecretPayloadBytes {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "secret data exceeds %d KiB limit", maxSecretPayloadBytes>>10)
	}
	switch secretType {
	case secretv1.SecretType_SECRET_TYPE_OPAQUE:
		return data, nil
	case secretv1.SecretType_SECRET_TYPE_DOCKER_CONFIG_JSON:
		if len(data) != 1 || data[dockerConfigJSONKey] == "" {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "docker-config-json secret must contain exactly the %q key", dockerConfigJSONKey)
		}
		if !json.Valid([]byte(data[dockerConfigJSONKey])) {
			return nil, grpcstatus.Error(codes.InvalidArgument, "docker-config-json secret payload must be valid JSON")
		}
		return data, nil
	default:
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "unsupported secret type %s", secretType.String())
	}
}

func normalizeNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "default"
	}
	return namespace
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func sortedKeys(in map[string]string) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}
