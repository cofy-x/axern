package domain

const LocalSchemaVersion = "axrun.local"

type KeyValue map[string]string

type SourceType string

const (
	SourceTypeInline SourceType = "inline"
	SourceTypeFile   SourceType = "file"
	SourceTypeDir    SourceType = "dir"
)

type SourceRef struct {
	Type       SourceType `json:"type,omitempty"`
	URI        string     `json:"uri,omitempty"`
	Path       string     `json:"path,omitempty"`
	SourcePath string     `json:"source_path,omitempty"`
}

type InputType string

const (
	InputTypeTaskSet InputType = "task_set"
)

type InputFormat string

const (
	InputFormatTaskSet InputFormat = "axrun.taskset.v1"
)

type InputSpec struct {
	Type         InputType    `json:"type,omitempty"`
	Format       InputFormat  `json:"format,omitempty"`
	URI          string       `json:"uri,omitempty"`
	Path         string       `json:"path,omitempty"`
	SourcePath   string       `json:"source_path,omitempty"`
	Digest       string       `json:"digest,omitempty"`
	SourceDigest string       `json:"source_digest,omitempty"`
	Payloads     []PayloadRef `json:"payloads,omitempty"`
}

type PayloadRef struct {
	Format    string `json:"format"`
	Reference string `json:"reference"`
	Digest    string `json:"digest"`
}

type TimeoutPolicy struct {
	AgentSec    int `json:"agent_sec,omitempty" yaml:"agent_sec,omitempty"`
	VerifierSec int `json:"verifier_sec,omitempty" yaml:"verifier_sec,omitempty"`
	EpisodeSec  int `json:"episode_sec,omitempty" yaml:"episode_sec,omitempty"`
}

type ResourceSpec struct {
	RequestCPU    string `json:"request_cpu,omitempty" yaml:"request_cpu,omitempty"`
	RequestMemory string `json:"request_memory,omitempty" yaml:"request_memory,omitempty"`
	LimitCPU      string `json:"limit_cpu,omitempty" yaml:"limit_cpu,omitempty"`
	LimitMemory   string `json:"limit_memory,omitempty" yaml:"limit_memory,omitempty"`
	Disk          string `json:"disk,omitempty" yaml:"disk,omitempty"`
}

type AgentRuntimeType string

type AgentApprovalPolicy string

const (
	AgentApprovalPolicyNever     AgentApprovalPolicy = "never"
	AgentApprovalPolicyOnRequest AgentApprovalPolicy = "on_request"
)

const (
	AgentRuntimeTypeAgentImage     AgentRuntimeType = "agent-image"
	AgentRuntimeTypeSandboxCommand AgentRuntimeType = "sandbox-command"
	AgentRuntimeTypeOracle         AgentRuntimeType = "oracle"
)

type AgentRuntimeSpec struct {
	Type           AgentRuntimeType    `json:"type,omitempty"`
	Image          string              `json:"image,omitempty"`
	MountTarget    string              `json:"mount_target,omitempty"`
	BinDir         string              `json:"bin_dir,omitempty"`
	Entrypoint     []string            `json:"entrypoint,omitempty"`
	Command        []string            `json:"command,omitempty"`
	Args           []string            `json:"args,omitempty"`
	Workdir        string              `json:"workdir,omitempty"`
	User           string              `json:"user,omitempty"`
	TimeoutSec     int                 `json:"timeout_sec,omitempty"`
	MaxTurns       int                 `json:"max_turns,omitempty"`
	OutputFormat   string              `json:"output_format,omitempty"`
	AllowedTools   []string            `json:"allowed_tools,omitempty"`
	IdleTimeoutSec int                 `json:"idle_timeout_sec,omitempty"`
	Env            map[string]string   `json:"env,omitempty"`
	Profile        string              `json:"profile,omitempty"`
	Prompt         *PromptSpec         `json:"prompt,omitempty"`
	Session        *AgentSessionSpec   `json:"session,omitempty"`
	Capabilities   []string            `json:"capabilities,omitempty"`
	Artifacts      *ArtifactPolicySpec `json:"artifacts,omitempty"`
}

type ArtifactPolicySpec struct {
	PatchPath     string   `json:"patch_path,omitempty"`
	PatchRequired bool     `json:"patch_required,omitempty"`
	OutputPaths   []string `json:"output_paths,omitempty"`
	CaptureStdout bool     `json:"capture_stdout,omitempty"`
	CaptureStderr bool     `json:"capture_stderr,omitempty"`
	CaptureRawLog bool     `json:"capture_raw_log,omitempty"`
}

type InitialStateSpec struct {
	Type           string                    `json:"type,omitempty"`
	Path           string                    `json:"path,omitempty"`
	Image          string                    `json:"image,omitempty"`
	Dockerfile     string                    `json:"dockerfile,omitempty"`
	Workdir        string                    `json:"workdir,omitempty"`
	Files          []string                  `json:"files,omitempty"`
	ExcludePaths   []string                  `json:"exclude_paths,omitempty"`
	WorkspaceImage *WorkspaceImageSourceSpec `json:"workspace_image,omitempty"`
}

type WorkspaceImageSourceSpec struct {
	Variants   []WorkspaceImageVariantSpec `json:"variants"`
	SourcePath string                      `json:"source_path"`
	Target     string                      `json:"target"`
}

type WorkspaceImageVariantSpec struct {
	Format string `json:"format"`
	Image  string `json:"image"`
}

type SandboxRuntimeSourceType string

const (
	SandboxRuntimeSourceTemplate   SandboxRuntimeSourceType = "template"
	SandboxRuntimeSourceImage      SandboxRuntimeSourceType = "image"
	SandboxRuntimeSourceDockerfile SandboxRuntimeSourceType = "dockerfile"
)

type SandboxRuntimeSourceSpec struct {
	Type       SandboxRuntimeSourceType `json:"type" yaml:"type"`
	TemplateID string                   `json:"template_id,omitempty" yaml:"template_id,omitempty"`
	Image      string                   `json:"image,omitempty" yaml:"image,omitempty"`
	Dockerfile string                   `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Origin     string                   `json:"origin,omitempty" yaml:"origin,omitempty"`
}

const (
	SandboxRuntimeSourceOriginAxernEnvTemplateID         = "axern.env.template_id"
	SandboxRuntimeSourceOriginAxernEnvImage              = "axern.env.image"
	SandboxRuntimeSourceOriginRuntimeImageBuild          = "runtime_image.build"
	SandboxRuntimeSourceOriginTaskInitImage              = "task.init.image"
	SandboxRuntimeSourceOriginTaskInitTemplate           = "task.init.template"
	SandboxRuntimeSourceOriginTaskInitialStateImage      = "task.initial_state.image"
	SandboxRuntimeSourceOriginTaskInitialStateDockerfile = "task.initial_state.dockerfile"
)

type OracleSpec struct {
	Type    string `json:"type,omitempty" yaml:"type,omitempty"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
}

type VerifierAssetSpec struct {
	Path       string `json:"path" yaml:"path"`
	TargetPath string `json:"target_path,omitempty" yaml:"target_path,omitempty"`
}

type SandboxRuntimeState struct {
	EnvironmentID         string `json:"environment_id,omitempty"`
	ServiceID             string `json:"service_id,omitempty"`
	AllocationID          string `json:"allocation_id,omitempty"`
	NodeID                string `json:"node_id,omitempty"`
	RuntimeClass          string `json:"runtime_class,omitempty"`
	PayloadFormat         string `json:"payload_format,omitempty"`
	PayloadDigest         string `json:"payload_digest,omitempty"`
	CacheHit              bool   `json:"cache_hit,omitempty"`
	ImageResolveMs        int64  `json:"image_resolve_ms,omitempty"`
	ImagePullMs           int64  `json:"image_pull_ms,omitempty"`
	CowPrepareMs          int64  `json:"cow_prepare_ms,omitempty"`
	VerifierMaterializeMs int64  `json:"verifier_materialize_ms,omitempty"`
}
