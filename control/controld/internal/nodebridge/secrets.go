package nodebridge

import (
	"context"
	"fmt"
	"strings"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

type secretReferenceContext struct {
	field    string
	target   string
	secretID string
	key      string
}

func resolveExecutionSecrets(ctx context.Context, secrets secretkernel.ValueResolver, credentials environmentkernel.RegistryCredentialResolver, config *commonv1.ExecutionConfig, env *environmentv1.Environment) (resolvedExecutionSecrets, error) {
	cfg := configOrEmpty(config)
	out := resolvedExecutionSecrets{}
	for _, item := range cfg.GetSecretEnv() {
		if item == nil {
			continue
		}
		value, ok, err := resolveSecretValue(ctx, secrets, secretReferenceContext{
			field:    "config.secret_env",
			target:   strings.TrimSpace(item.GetName()),
			secretID: item.GetSecretID(),
			key:      item.GetKey(),
		}, item.GetOptional())
		if err != nil {
			return resolvedExecutionSecrets{}, err
		}
		if !ok {
			continue
		}
		out.EnvSecrets = append(out.EnvSecrets, &privatenodev1.ResolvedSecretEnvVar{
			Name:  strings.TrimSpace(item.GetName()),
			Value: value,
		})
	}
	for _, item := range cfg.GetSecretFiles() {
		if item == nil {
			continue
		}
		value, ok, err := resolveSecretValue(ctx, secrets, secretReferenceContext{
			field:    "config.secret_files",
			target:   strings.TrimSpace(item.GetPath()),
			secretID: item.GetSecretID(),
			key:      item.GetKey(),
		}, item.GetOptional())
		if err != nil {
			return resolvedExecutionSecrets{}, err
		}
		if !ok {
			continue
		}
		mode := item.GetMode()
		if mode == 0 {
			mode = 0o400
		}
		out.FileSecrets = append(out.FileSecrets, &privatenodev1.ResolvedSecretFile{
			Path:    strings.TrimSpace(item.GetPath()),
			Content: []byte(value),
			Mode:    mode,
		})
	}
	registryCredentialID := strings.TrimSpace(env.GetSpec().GetImage().GetRegistryCredentialID())
	if registryCredentialID != "" {
		if credentials == nil {
			return resolvedExecutionSecrets{}, fmt.Errorf("environment.image.registry_credential_id %q cannot be resolved because registry credential resolution is not configured", registryCredentialID)
		}
		dockerConfigJSON, ok, err := credentials.ResolveDockerConfigJSON(ctx, registryCredentialID)
		if err != nil {
			return resolvedExecutionSecrets{}, fmt.Errorf("resolve environment.image.registry_credential_id %q: %w", registryCredentialID, err)
		}
		if !ok {
			return resolvedExecutionSecrets{}, fmt.Errorf("environment.image.registry_credential_id %q not found", registryCredentialID)
		}
		out.DockerConfigJSON = dockerConfigJSON
	}
	return out, nil
}

func resolveSecretValue(ctx context.Context, resolver secretkernel.ValueResolver, ref secretReferenceContext, optional bool) (string, bool, error) {
	if resolver == nil {
		if optional {
			return "", false, nil
		}
		return "", false, fmt.Errorf("%s %q references secret %q, but secret resolution is not configured", ref.field, ref.target, ref.secretID)
	}
	resolved, ok, err := resolver.Resolve(ctx, strings.TrimSpace(ref.secretID))
	if err != nil {
		return "", false, fmt.Errorf("resolve %s %q from secret %q key %q: %w", ref.field, ref.target, ref.secretID, ref.key, err)
	}
	if !ok {
		if optional {
			return "", false, nil
		}
		return "", false, fmt.Errorf("%s %q references secret %q, but it was not found", ref.field, ref.target, ref.secretID)
	}
	value, exists := resolved.Data[strings.TrimSpace(ref.key)]
	if !exists {
		if optional {
			return "", false, nil
		}
		return "", false, fmt.Errorf("%s %q references secret %q key %q, but that key was not found", ref.field, ref.target, ref.secretID, ref.key)
	}
	return value, true, nil
}
