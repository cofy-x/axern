package resourcespec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/apps/cli/internal/parse"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"gopkg.in/yaml.v3"
)

const APIVersion = "axern/v1"

type Kind string

const (
	KindRun Kind = "Run"
)

type Envelope struct {
	APIVersion string   `json:"api_version" yaml:"api_version"`
	Kind       Kind     `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
	Path       string   `json:"-" yaml:"-"`
}

type Metadata struct {
	Name      string            `json:"name,omitempty" yaml:"name,omitempty"`
	Namespace string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type Spec struct {
	Source                Source            `json:"source" yaml:"source"`
	Command               Command           `json:"command,omitempty" yaml:"command,omitempty"`
	ExtensionCapabilities map[string]string `json:"extension_capabilities,omitempty" yaml:"extension_capabilities,omitempty"`
	Resources             Resources         `json:"resources,omitempty" yaml:"resources,omitempty"`
	Env                   map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	SecretEnv             []SecretEnv       `json:"secret_env,omitempty" yaml:"secret_env,omitempty"`
	SecretFiles           []SecretFile      `json:"secret_files,omitempty" yaml:"secret_files,omitempty"`
	ImageMounts           []ImageMount      `json:"image_mounts,omitempty" yaml:"image_mounts,omitempty"`
	RootfsSnapshot        bool              `json:"rootfs_snapshot,omitempty" yaml:"rootfs_snapshot,omitempty"`
}

type Source struct {
	Environment          string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Image                string `json:"image,omitempty" yaml:"image,omitempty"`
	RegistryCredentialID string `json:"registry_credential_id,omitempty" yaml:"registry_credential_id,omitempty"`
	RootFSReadonly       bool   `json:"rootfs_readonly,omitempty" yaml:"rootfs_readonly,omitempty"`
}

type Command struct {
	Argv []string `json:"argv,omitempty" yaml:"argv,omitempty"`
	Cwd  string   `json:"cwd,omitempty" yaml:"cwd,omitempty"`
}

type Resources struct {
	Requests Quantity `json:"requests,omitempty" yaml:"requests,omitempty"`
	Limits   Quantity `json:"limits,omitempty" yaml:"limits,omitempty"`
}

type Quantity struct {
	CPU              string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory           string `json:"memory,omitempty" yaml:"memory,omitempty"`
	EphemeralStorage string `json:"ephemeral_storage,omitempty" yaml:"ephemeral_storage,omitempty"`
}

type SecretEnv struct {
	Name     string `json:"name" yaml:"name"`
	SecretID string `json:"secret_id" yaml:"secret_id"`
	Key      string `json:"key" yaml:"key"`
	Optional bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
}

type SecretFile struct {
	Path     string `json:"path" yaml:"path"`
	SecretID string `json:"secret_id" yaml:"secret_id"`
	Key      string `json:"key" yaml:"key"`
	Mode     string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Optional bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
}

type ImageMount struct {
	Image  string `json:"image" yaml:"image"`
	Target string `json:"target" yaml:"target"`
}

func Load(path string, expected Kind) (*Envelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var envelope Envelope
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&envelope); err != nil {
			return nil, fmt.Errorf("parse resource spec %q: %w", path, err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parse resource spec %q: multiple JSON values", path)
		}
	default:
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&envelope); err != nil {
			return nil, fmt.Errorf("parse resource spec %q: %w", path, err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parse resource spec %q: multiple YAML documents", path)
		}
	}
	envelope.Path = path
	if err := envelope.Validate(expected); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func (e *Envelope) Validate(expected Kind) error {
	if e.APIVersion != APIVersion {
		return fmt.Errorf("api_version must be %q", APIVersion)
	}
	if e.Kind != expected {
		return fmt.Errorf("kind must be %q", expected)
	}
	if countNonEmpty(e.Spec.Source.Environment, e.Spec.Source.Image) != 1 {
		return fmt.Errorf("spec.source must select exactly one of environment or image")
	}
	if e.Spec.Source.Image == "" && (e.Spec.Source.RegistryCredentialID != "" || e.Spec.Source.RootFSReadonly) {
		return fmt.Errorf("spec.source registry_credential_id and rootfs_readonly require image")
	}
	if e.Metadata.Namespace == "" {
		e.Metadata.Namespace = "default"
	}
	if _, err := e.ResourceSpec(); err != nil {
		return err
	}
	if _, _, _, err := e.secretAndImageMounts(); err != nil {
		return err
	}
	return nil
}

func (e Envelope) EnvironmentSpec() (string, *environmentv1.EnvironmentSpec) {
	if e.Spec.Source.Environment != "" {
		return e.Spec.Source.Environment, nil
	}
	spec := &environmentv1.EnvironmentSpec{Namespace: e.Metadata.Namespace,
		Image: &environmentv1.EnvironmentImageSource{
			Ref:                  e.Spec.Source.Image,
			RegistryCredentialID: e.Spec.Source.RegistryCredentialID,
			RootfsReadonly:       e.Spec.Source.RootFSReadonly,
		}}
	return "", spec
}

func (e Envelope) ExecutionConfig() (*commonv1.ExecutionConfig, error) {
	resources, err := e.ResourceSpec()
	if err != nil {
		return nil, err
	}
	secretEnv, secretFiles, imageMounts, err := e.secretAndImageMounts()
	if err != nil {
		return nil, err
	}
	extensionValues := make([]string, 0, len(e.Spec.ExtensionCapabilities))
	for name, value := range e.Spec.ExtensionCapabilities {
		extensionValues = append(extensionValues, name+"="+value)
	}
	sort.Strings(extensionValues)
	extensions, err := parse.ExtensionCapabilities(extensionValues)
	if err != nil {
		return nil, err
	}
	config := &commonv1.ExecutionConfig{
		Argv:                            append([]string(nil), e.Spec.Command.Argv...),
		Cwd:                             e.Spec.Command.Cwd,
		Env:                             cloneMap(e.Spec.Env),
		ExtensionCapabilityRequirements: extensions,
		Resources:                       resources,
		SecretEnv:                       secretEnv,
		SecretFiles:                     secretFiles,
		ImageMounts:                     imageMounts,
	}
	if e.Spec.RootfsSnapshot {
		config.RootfsSnapshot = &commonv1.RootfsSnapshot{}
	}
	return config, nil
}

func (e Envelope) secretAndImageMounts() ([]*commonv1.SecretEnvVar, []*commonv1.SecretFile, []*commonv1.ImageMount, error) {
	envNames := make(map[string]struct{}, len(e.Spec.SecretEnv))
	secretEnv := make([]*commonv1.SecretEnvVar, 0, len(e.Spec.SecretEnv))
	for index, item := range e.Spec.SecretEnv {
		if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.SecretID) == "" || strings.TrimSpace(item.Key) == "" {
			return nil, nil, nil, fmt.Errorf("spec.secret_env[%d] requires name, secret_id, and key", index)
		}
		if _, exists := envNames[item.Name]; exists {
			return nil, nil, nil, fmt.Errorf("spec.secret_env[%d].name is duplicated", index)
		}
		envNames[item.Name] = struct{}{}
		secretEnv = append(secretEnv, &commonv1.SecretEnvVar{Name: item.Name, SecretID: item.SecretID, Key: item.Key, Optional: item.Optional})
	}

	filePaths := make(map[string]struct{}, len(e.Spec.SecretFiles))
	secretFiles := make([]*commonv1.SecretFile, 0, len(e.Spec.SecretFiles))
	for index, item := range e.Spec.SecretFiles {
		if !validTarget(item.Path) || strings.TrimSpace(item.SecretID) == "" || strings.TrimSpace(item.Key) == "" {
			return nil, nil, nil, fmt.Errorf("spec.secret_files[%d] requires an absolute path below /, secret_id, and key", index)
		}
		if _, exists := filePaths[item.Path]; exists {
			return nil, nil, nil, fmt.Errorf("spec.secret_files[%d].path is duplicated", index)
		}
		mode := uint64(0)
		if item.Mode != "" {
			var err error
			mode, err = strconv.ParseUint(item.Mode, 8, 32)
			if err != nil || mode > 0o777 {
				return nil, nil, nil, fmt.Errorf("spec.secret_files[%d].mode must be an octal permission string", index)
			}
		}
		filePaths[item.Path] = struct{}{}
		secretFiles = append(secretFiles, &commonv1.SecretFile{Path: item.Path, SecretID: item.SecretID, Key: item.Key, Mode: uint32(mode), Optional: item.Optional})
	}

	imageTargets := make(map[string]struct{}, len(e.Spec.ImageMounts))
	imageMounts := make([]*commonv1.ImageMount, 0, len(e.Spec.ImageMounts))
	for index, item := range e.Spec.ImageMounts {
		if strings.TrimSpace(item.Image) == "" || !validTarget(item.Target) {
			return nil, nil, nil, fmt.Errorf("spec.image_mounts[%d] requires image and an absolute target below /", index)
		}
		if _, exists := imageTargets[item.Target]; exists {
			return nil, nil, nil, fmt.Errorf("spec.image_mounts[%d].target is duplicated", index)
		}
		imageTargets[item.Target] = struct{}{}
		imageMounts = append(imageMounts, &commonv1.ImageMount{Image: item.Image, Target: item.Target})
	}
	return secretEnv, secretFiles, imageMounts, nil
}

func validTarget(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value && value != string(filepath.Separator)
}

func (e Envelope) ResourceSpec() (*commonv1.ResourceSpec, error) {
	requests, err := quantity(e.Spec.Resources.Requests)
	if err != nil {
		return nil, fmt.Errorf("spec.resources.requests: %w", err)
	}
	limits, err := quantity(e.Spec.Resources.Limits)
	if err != nil {
		return nil, fmt.Errorf("spec.resources.limits: %w", err)
	}
	if requests == nil && limits == nil {
		return nil, nil
	}
	return &commonv1.ResourceSpec{Requests: requests, Limits: limits}, nil
}

func quantity(value Quantity) (*commonv1.ResourceQuantity, error) {
	cpu, err := parse.CPU(value.CPU)
	if err != nil {
		return nil, err
	}
	memory, err := parse.Memory(value.Memory)
	if err != nil {
		return nil, err
	}
	ephemeralStorage, err := parse.Memory(value.EphemeralStorage)
	if err != nil {
		return nil, err
	}
	if cpu == 0 && memory == 0 && ephemeralStorage == 0 {
		return nil, nil
	}
	return &commonv1.ResourceQuantity{CpuMilli: cpu, MemoryBytes: memory, EphemeralStorageBytes: ephemeralStorage}, nil
}

func countNonEmpty(values ...string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func cloneMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func RejectDefinitionFlags(isSet func(string) bool, names ...string) error {
	for _, name := range names {
		if isSet(name) {
			return fmt.Errorf("--file cannot be combined with --%s", name)
		}
	}
	return nil
}
