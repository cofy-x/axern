package parse

import (
	"fmt"
	"strconv"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
)

const validSecretTypes = "opaque, docker-config-json"

func SecretEnvVars(values []string) ([]*commonv1.SecretEnvVar, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*commonv1.SecretEnvVar, 0, len(values))
	for _, value := range values {
		name, ref, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("invalid secret env %q, want NAME=secret_id:key", value)
		}
		secretID, key, ok := strings.Cut(ref, ":")
		if !ok || strings.TrimSpace(secretID) == "" || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid secret env %q, want NAME=secret_id:key", value)
		}
		out = append(out, &commonv1.SecretEnvVar{
			Name:     strings.TrimSpace(name),
			SecretID: strings.TrimSpace(secretID),
			Key:      strings.TrimSpace(key),
		})
	}
	return out, nil
}

func SecretFiles(values []string) ([]*commonv1.SecretFile, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*commonv1.SecretFile, 0, len(values))
	for _, value := range values {
		path, ref, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("invalid secret file %q, want /path=secret_id:key[:mode]", value)
		}
		secretID, keyAndMode, ok := strings.Cut(ref, ":")
		if !ok || strings.TrimSpace(secretID) == "" {
			return nil, fmt.Errorf("invalid secret file %q, want /path=secret_id:key[:mode]", value)
		}
		key, modeText, _ := strings.Cut(keyAndMode, ":")
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid secret file %q, want /path=secret_id:key[:mode]", value)
		}
		file := &commonv1.SecretFile{
			Path:     strings.TrimSpace(path),
			SecretID: strings.TrimSpace(secretID),
			Key:      strings.TrimSpace(key),
		}
		if strings.TrimSpace(modeText) != "" {
			mode, err := strconv.ParseUint(strings.TrimSpace(modeText), 8, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid secret file mode %q", modeText)
			}
			file.Mode = uint32(mode)
		}
		out = append(out, file)
	}
	return out, nil
}

func SecretType(value string) (secretv1.SecretType, error) {
	parsed, err := SecretTypeAllowEmpty(value)
	if err != nil {
		return secretv1.SecretType_SECRET_TYPE_UNSPECIFIED, err
	}
	if parsed == secretv1.SecretType_SECRET_TYPE_UNSPECIFIED {
		return secretv1.SecretType_SECRET_TYPE_UNSPECIFIED, fmt.Errorf("secret type must be one of: %s", validSecretTypes)
	}
	return parsed, nil
}

func SecretTypeAllowEmpty(value string) (secretv1.SecretType, error) {
	switch normalizeToken(value) {
	case "":
		return secretv1.SecretType_SECRET_TYPE_UNSPECIFIED, nil
	case "opaque":
		return secretv1.SecretType_SECRET_TYPE_OPAQUE, nil
	case "dockerconfigjson", "docker-config-json":
		return secretv1.SecretType_SECRET_TYPE_DOCKER_CONFIG_JSON, nil
	default:
		return secretv1.SecretType_SECRET_TYPE_UNSPECIFIED, fmt.Errorf("invalid secret type %q, want one of: %s", value, validSecretTypes)
	}
}
