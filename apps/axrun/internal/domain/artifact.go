package domain

import "time"

type ArtifactKind string

const (
	ArtifactKindAgentRawLog        ArtifactKind = "agent_raw_log"
	ArtifactKindAgentStdout        ArtifactKind = "agent_stdout"
	ArtifactKindAgentStderr        ArtifactKind = "agent_stderr"
	ArtifactKindDownloadedFile     ArtifactKind = "downloaded_file"
	ArtifactKindDownloadedDir      ArtifactKind = "downloaded_directory"
	ArtifactKindLLMTelemetry       ArtifactKind = "llm_telemetry"
	ArtifactKindPatch              ArtifactKind = "patch"
	ArtifactKindRuntimeImageBuild  ArtifactKind = "runtime_image_build"
	ArtifactKindTrajectoryExport   ArtifactKind = "trajectory_export"
	ArtifactKindTrainingDataExport ArtifactKind = "training_data_export"
	ArtifactKindVerifierBreakdown  ArtifactKind = "verifier_breakdown"
)

type ArtifactRole string

const (
	ArtifactRoleInput   ArtifactRole = "input"
	ArtifactRoleOutput  ArtifactRole = "output"
	ArtifactRoleRaw     ArtifactRole = "raw"
	ArtifactRoleDerived ArtifactRole = "derived"
	ArtifactRoleExport  ArtifactRole = "export"
)

type ArtifactRef struct {
	Path        string       `json:"path"`
	Kind        ArtifactKind `json:"kind,omitempty"`
	Description string       `json:"description,omitempty"`
	SHA256      string       `json:"sha256,omitempty"`
	MediaType   string       `json:"media_type,omitempty"`
	SizeBytes   int64        `json:"size_bytes,omitempty"`
	CreatedAt   *time.Time   `json:"created_at,omitempty"`
	Producer    string       `json:"producer,omitempty"`
	Role        ArtifactRole `json:"role,omitempty"`
}
