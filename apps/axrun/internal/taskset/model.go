package taskset

import "github.com/cofy-x/axern/apps/axrun/internal/domain"

const (
	APIVersion          = "axrun/v1"
	BuildKind           = "TaskSetBuild"
	DescriptorKind      = "TaskSet"
	DescriptorMediaType = "application/vnd.axrun.taskset.v1+json"
	PackerVersion       = "deterministic-tar-v1"
)

type BuildEnvelope struct {
	APIVersion string    `json:"api_version" yaml:"api_version"`
	Kind       string    `json:"kind" yaml:"kind"`
	Metadata   Metadata  `json:"metadata" yaml:"metadata"`
	Spec       BuildSpec `json:"spec" yaml:"spec"`
}

type Metadata struct {
	Name string `json:"name" yaml:"name"`
}

type BuildSpec struct {
	Generators []Generator `json:"generators" yaml:"generators"`
}

type Generator struct {
	TaskID       string          `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	TaskIDPrefix string          `json:"task_id_prefix,omitempty" yaml:"task_id_prefix,omitempty"`
	Instruction  Instruction     `json:"instruction" yaml:"instruction"`
	Workspace    WorkspaceSource `json:"workspace" yaml:"workspace"`
	Task         TaskTemplate    `json:"task" yaml:"task"`
	ExcludePaths []string        `json:"exclude_paths,omitempty" yaml:"exclude_paths,omitempty"`
}

type Instruction struct {
	Text string `json:"text,omitempty" yaml:"text,omitempty"`
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

type WorkspaceSource struct {
	Paths  []string `json:"paths" yaml:"paths"`
	Expand string   `json:"expand" yaml:"expand"`
}

type TaskTemplate struct {
	Sandbox      domain.SandboxSpec    `json:"sandbox" yaml:"sandbox"`
	Verifier     domain.VerifierSpec   `json:"verifier" yaml:"verifier"`
	Outputs      []OutputContract      `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	Resources    *domain.ResourceSpec  `json:"resources,omitempty" yaml:"resources,omitempty"`
	Timeouts     *domain.TimeoutPolicy `json:"timeouts,omitempty" yaml:"timeouts,omitempty"`
	Oracle       *domain.OracleSpec    `json:"oracle,omitempty" yaml:"oracle,omitempty"`
	Tags         []string              `json:"tags,omitempty" yaml:"tags,omitempty"`
	Capabilities []string              `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
}

type OutputContract = domain.TaskOutputSpec

type Descriptor struct {
	APIVersion   string              `json:"api_version"`
	Kind         string              `json:"kind"`
	Name         string              `json:"name"`
	SourceDigest string              `json:"source_digest"`
	Provenance   Provenance          `json:"provenance"`
	Payloads     []PayloadDescriptor `json:"payloads,omitempty"`
	Tasks        []Task              `json:"tasks"`
}

type Provenance struct {
	Compiler      string `json:"compiler"`
	Contract      string `json:"contract"`
	PackerVersion string `json:"packer_version"`
}

type PayloadDescriptor struct {
	Format    string `json:"format"`
	Reference string `json:"reference"`
	Digest    string `json:"digest"`
	MediaType string `json:"media_type,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type Task struct {
	Instance         domain.TaskInstance `json:"instance"`
	WorkspaceSubpath string              `json:"workspace_subpath"`
	VerifierSubpath  string              `json:"verifier_subpath,omitempty"`
	OracleSubpath    string              `json:"oracle_subpath,omitempty"`
}

type BuildReceipt struct {
	SchemaVersion  string `json:"schema_version"`
	SourceDigest   string `json:"source_digest"`
	DescriptorPath string `json:"descriptor_path"`
	PayloadPath    string `json:"payload_path"`
	OCILayoutPath  string `json:"oci_layout_path"`
	TaskCount      int    `json:"task_count"`
}
