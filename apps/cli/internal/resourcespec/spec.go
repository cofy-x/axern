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
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/parse"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"gopkg.in/yaml.v3"
)

const APIVersion = "axern/v1"

type Kind string

const (
	KindRun      Kind = "Run"
	KindService  Kind = "Service"
	KindFunction Kind = "Function"
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
	RuntimeClass          string            `json:"runtime_class,omitempty" yaml:"runtime_class,omitempty"`
	ExtensionCapabilities map[string]string `json:"extension_capabilities,omitempty" yaml:"extension_capabilities,omitempty"`
	Resources             Resources         `json:"resources,omitempty" yaml:"resources,omitempty"`
	Replicas              *int32            `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Readiness             *Probe            `json:"readiness,omitempty" yaml:"readiness,omitempty"`
	Liveness              *Probe            `json:"liveness,omitempty" yaml:"liveness,omitempty"`
	Autoscaling           *Autoscaling      `json:"autoscaling,omitempty" yaml:"autoscaling,omitempty"`
	Function              *Function         `json:"function,omitempty" yaml:"function,omitempty"`
	Env                   map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	SecretEnv             []SecretEnv       `json:"secret_env,omitempty" yaml:"secret_env,omitempty"`
	SecretFiles           []SecretFile      `json:"secret_files,omitempty" yaml:"secret_files,omitempty"`
	Volumes               []Volume          `json:"volumes,omitempty" yaml:"volumes,omitempty"`
	ImageMounts           []ImageMount      `json:"image_mounts,omitempty" yaml:"image_mounts,omitempty"`
}

type Source struct {
	Environment          string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Template             string `json:"template,omitempty" yaml:"template,omitempty"`
	TemplateVersion      string `json:"template_version,omitempty" yaml:"template_version,omitempty"`
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

type Probe struct {
	HTTP             *HTTPProbe `json:"http,omitempty" yaml:"http,omitempty"`
	TCPPort          int32      `json:"tcp_port,omitempty" yaml:"tcp_port,omitempty"`
	InitialDelay     string     `json:"initial_delay,omitempty" yaml:"initial_delay,omitempty"`
	Period           string     `json:"period,omitempty" yaml:"period,omitempty"`
	Timeout          string     `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	SuccessThreshold int32      `json:"success_threshold,omitempty" yaml:"success_threshold,omitempty"`
	FailureThreshold int32      `json:"failure_threshold,omitempty" yaml:"failure_threshold,omitempty"`
}

type HTTPProbe struct {
	Port   int32  `json:"port" yaml:"port"`
	Path   string `json:"path,omitempty" yaml:"path,omitempty"`
	Scheme string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
}

type Autoscaling struct {
	MinReplicas int32 `json:"min_replicas" yaml:"min_replicas"`
	MaxReplicas int32 `json:"max_replicas" yaml:"max_replicas"`
}

type Function struct {
	Runtime        string  `json:"runtime" yaml:"runtime"`
	Handler        string  `json:"handler" yaml:"handler"`
	Initializer    string  `json:"initializer,omitempty" yaml:"initializer,omitempty"`
	Source         string  `json:"source" yaml:"source"`
	TimeoutSeconds int     `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
	Scaling        Scaling `json:"scaling,omitempty" yaml:"scaling,omitempty"`
}

type Scaling struct {
	MinReplicas int32  `json:"min_replicas,omitempty" yaml:"min_replicas,omitempty"`
	MaxReplicas int32  `json:"max_replicas,omitempty" yaml:"max_replicas,omitempty"`
	Concurrency int32  `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	IdleTimeout string `json:"idle_timeout,omitempty" yaml:"idle_timeout,omitempty"`
}

type Volume struct {
	Name     string   `json:"name" yaml:"name"`
	Target   string   `json:"target" yaml:"target"`
	Readonly bool     `json:"readonly,omitempty" yaml:"readonly,omitempty"`
	Options  []string `json:"options,omitempty" yaml:"options,omitempty"`
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
	if countNonEmpty(e.Spec.Source.Environment, e.Spec.Source.Template, e.Spec.Source.Image) != 1 {
		return fmt.Errorf("spec.source must select exactly one of environment, template, or image")
	}
	if e.Spec.Source.Template == "" && e.Spec.Source.TemplateVersion != "" {
		return fmt.Errorf("spec.source.template_version requires template")
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
	switch e.Kind {
	case KindRun:
		if e.Spec.Replicas != nil || e.Spec.Readiness != nil || e.Spec.Liveness != nil || e.Spec.Autoscaling != nil || e.Spec.Function != nil {
			return fmt.Errorf("Run spec contains service or function fields")
		}
	case KindService:
		if e.Spec.Replicas != nil && *e.Spec.Replicas < 0 || e.Spec.Function != nil {
			return fmt.Errorf("Service replicas must be non-negative and function must be omitted")
		}
		if e.Spec.Autoscaling != nil && (e.Spec.Autoscaling.MinReplicas < 0 || e.Spec.Autoscaling.MaxReplicas < e.Spec.Autoscaling.MinReplicas) {
			return fmt.Errorf("Service autoscaling range is invalid")
		}
		if _, err := probe(e.Spec.Readiness); err != nil {
			return fmt.Errorf("spec.readiness: %w", err)
		}
		if _, err := probe(e.Spec.Liveness); err != nil {
			return fmt.Errorf("spec.liveness: %w", err)
		}
	case KindFunction:
		if e.Metadata.Name == "" || e.Spec.Function == nil {
			return fmt.Errorf("Function metadata.name and spec.function are required")
		}
		if e.Spec.Replicas != nil || e.Spec.Readiness != nil || e.Spec.Liveness != nil || e.Spec.Autoscaling != nil {
			return fmt.Errorf("Function spec contains service fields")
		}
		if strings.TrimSpace(e.Spec.Function.Runtime) == "" || strings.TrimSpace(e.Spec.Function.Handler) == "" || strings.TrimSpace(e.Spec.Function.Source) == "" {
			return fmt.Errorf("Function runtime, handler, and source are required")
		}
		if len(e.Spec.Command.Argv) != 0 || e.Spec.Command.Cwd != "" || e.Spec.RuntimeClass != "" || len(e.Spec.SecretEnv) != 0 || len(e.Spec.SecretFiles) != 0 || len(e.Spec.ImageMounts) != 0 {
			return fmt.Errorf("Function command, runtime_class, secret mounts, and image mounts are owned by the function runtime")
		}
		if e.Spec.Function.TimeoutSeconds == 0 {
			e.Spec.Function.TimeoutSeconds = 60
		}
		if e.Spec.Function.TimeoutSeconds < 0 {
			return fmt.Errorf("Function timeout_seconds must be greater than zero")
		}
		if e.Spec.Function.Scaling.MaxReplicas == 0 {
			e.Spec.Function.Scaling.MaxReplicas = 1
		}
		if e.Spec.Function.Scaling.Concurrency == 0 {
			e.Spec.Function.Scaling.Concurrency = 1
		}
		if e.Spec.Function.Scaling.IdleTimeout == "" {
			e.Spec.Function.Scaling.IdleTimeout = "5m"
		}
		if _, err := e.FunctionSourcePath(); err != nil {
			return err
		}
		scaling := e.Spec.Function.Scaling
		if scaling.MinReplicas < 0 || scaling.MaxReplicas < scaling.MinReplicas || scaling.Concurrency < 1 {
			return fmt.Errorf("Function scaling values are invalid")
		}
		if _, err := parseDuration(scaling.IdleTimeout); err != nil {
			return fmt.Errorf("spec.function.scaling.idle_timeout: %w", err)
		}
	}
	if _, err := e.VolumeMounts(); err != nil {
		return err
	}
	if _, _, _, err := e.secretAndImageMounts(); err != nil {
		return err
	}
	return nil
}

func (e Envelope) ServiceReplicas() int32 {
	if e.Spec.Replicas == nil {
		return 1
	}
	return *e.Spec.Replicas
}

func (e Envelope) ServiceConfig() (*servicev1.ServiceProbe, *servicev1.ServiceProbe, *servicev1.ServiceAutoscalingPolicy, error) {
	readiness, err := probe(e.Spec.Readiness)
	if err != nil {
		return nil, nil, nil, err
	}
	liveness, err := probe(e.Spec.Liveness)
	if err != nil {
		return nil, nil, nil, err
	}
	var autoscaling *servicev1.ServiceAutoscalingPolicy
	if e.Spec.Autoscaling != nil {
		autoscaling = &servicev1.ServiceAutoscalingPolicy{MinReplicas: e.Spec.Autoscaling.MinReplicas, MaxReplicas: e.Spec.Autoscaling.MaxReplicas}
	}
	return readiness, liveness, autoscaling, nil
}

func (e Envelope) EnvironmentSpec() (string, *environmentv1.EnvironmentSpec) {
	if e.Spec.Source.Environment != "" {
		return e.Spec.Source.Environment, nil
	}
	spec := &environmentv1.EnvironmentSpec{Namespace: e.Metadata.Namespace}
	if e.Spec.Source.Template != "" {
		spec.TemplateID = e.Spec.Source.Template
		spec.TemplateVersion = e.Spec.Source.TemplateVersion
	} else {
		spec.Image = &environmentv1.EnvironmentImageSource{
			Ref:                  e.Spec.Source.Image,
			RegistryCredentialID: e.Spec.Source.RegistryCredentialID,
			RootfsReadonly:       e.Spec.Source.RootFSReadonly,
		}
	}
	return "", spec
}

func (e Envelope) ExecutionConfig() (*commonv1.ExecutionConfig, error) {
	resources, err := e.ResourceSpec()
	if err != nil {
		return nil, err
	}
	volumes, err := e.VolumeMounts()
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
	return &commonv1.ExecutionConfig{
		Argv:                            append([]string(nil), e.Spec.Command.Argv...),
		Cwd:                             e.Spec.Command.Cwd,
		Env:                             cloneMap(e.Spec.Env),
		RuntimeClass:                    e.Spec.RuntimeClass,
		ExtensionCapabilityRequirements: extensions,
		Resources:                       resources,
		VolumeMounts:                    volumes,
		SecretEnv:                       secretEnv,
		SecretFiles:                     secretFiles,
		ImageMounts:                     imageMounts,
	}, nil
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
		imageMounts = append(imageMounts, &commonv1.ImageMount{Image: item.Image, Target: item.Target, Readonly: true})
	}
	return secretEnv, secretFiles, imageMounts, nil
}

func validTarget(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value && value != string(filepath.Separator)
}

func (e Envelope) VolumeMounts() ([]*commonv1.ServiceVolumeMount, error) {
	if len(e.Spec.Volumes) == 0 {
		return nil, nil
	}
	seenNames := make(map[string]struct{}, len(e.Spec.Volumes))
	seenTargets := make(map[string]struct{}, len(e.Spec.Volumes))
	out := make([]*commonv1.ServiceVolumeMount, 0, len(e.Spec.Volumes))
	for index, volume := range e.Spec.Volumes {
		if strings.TrimSpace(volume.Name) == "" {
			return nil, fmt.Errorf("spec.volumes[%d].name is required", index)
		}
		if !validTarget(volume.Target) {
			return nil, fmt.Errorf("spec.volumes[%d].target must be an absolute path below /", index)
		}
		if _, ok := seenNames[volume.Name]; ok {
			return nil, fmt.Errorf("spec.volumes[%d].name is duplicated", index)
		}
		if _, ok := seenTargets[volume.Target]; ok {
			return nil, fmt.Errorf("spec.volumes[%d].target is duplicated", index)
		}
		seenNames[volume.Name] = struct{}{}
		seenTargets[volume.Target] = struct{}{}
		out = append(out, &commonv1.ServiceVolumeMount{Name: volume.Name, Target: volume.Target, Readonly: volume.Readonly, Options: append([]string(nil), volume.Options...)})
	}
	return out, nil
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

func (e Envelope) FunctionSourcePath() (string, error) {
	if e.Spec.Function == nil {
		return "", fmt.Errorf("spec.function is required")
	}
	if filepath.IsAbs(e.Spec.Function.Source) {
		return "", fmt.Errorf("spec.function.source must be relative to the spec file")
	}
	base, err := filepath.Abs(filepath.Dir(e.Path))
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(base, e.Spec.Function.Source))
	if err != nil {
		return "", err
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(resolvedBase, resolvedPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("spec.function.source must stay below the spec directory")
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("spec.function.source must be a directory")
	}
	return resolvedPath, nil
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

func probe(value *Probe) (*servicev1.ServiceProbe, error) {
	if value == nil {
		return nil, nil
	}
	if (value.HTTP == nil) == (value.TCPPort == 0) {
		return nil, fmt.Errorf("exactly one of http or tcp_port is required")
	}
	if value.SuccessThreshold < 0 || value.FailureThreshold < 0 {
		return nil, fmt.Errorf("success_threshold and failure_threshold must be non-negative")
	}
	out := &servicev1.ServiceProbe{
		SuccessThreshold: value.SuccessThreshold,
		FailureThreshold: value.FailureThreshold,
	}
	var err error
	if out.InitialDelay, err = parseDuration(value.InitialDelay); err != nil {
		return nil, fmt.Errorf("initial_delay: %w", err)
	}
	if out.Period, err = parseDuration(value.Period); err != nil {
		return nil, fmt.Errorf("period: %w", err)
	}
	if out.Timeout, err = parseDuration(value.Timeout); err != nil {
		return nil, fmt.Errorf("timeout: %w", err)
	}
	if value.HTTP != nil {
		if value.HTTP.Port <= 0 || value.HTTP.Port > 65535 {
			return nil, fmt.Errorf("http.port must be between 1 and 65535")
		}
		scheme := servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTP
		switch strings.ToLower(strings.TrimSpace(value.HTTP.Scheme)) {
		case "", "http":
		case "https":
			scheme = servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTPS
		default:
			return nil, fmt.Errorf("http.scheme must be http or https")
		}
		out.Action = &servicev1.ServiceProbe_Http{Http: &servicev1.HttpProbe{Port: value.HTTP.Port, Path: value.HTTP.Path, Scheme: scheme}}
	} else {
		if value.TCPPort <= 0 || value.TCPPort > 65535 {
			return nil, fmt.Errorf("tcp_port must be between 1 and 65535")
		}
		out.Action = &servicev1.ServiceProbe_Tcp{Tcp: &servicev1.TcpProbe{Port: value.TCPPort}}
	}
	return out, nil
}

func parseDuration(value string) (*durationpb.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return nil, fmt.Errorf("must be a non-negative duration")
	}
	return durationpb.New(duration), nil
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
