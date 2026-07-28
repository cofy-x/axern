package domain

import "time"

type RolloutPhase string

const (
	RolloutPhasePlanning        RolloutPhase = "planning"
	RolloutPhasePreparingInputs RolloutPhase = "preparing_inputs"
	RolloutPhaseSandboxCreating RolloutPhase = "sandbox_creating"
	RolloutPhaseAgentRunning    RolloutPhase = "agent_running"
	RolloutPhaseVerifying       RolloutPhase = "verifying"
	RolloutPhaseCollecting      RolloutPhase = "collecting"
	RolloutPhaseValidating      RolloutPhase = "validating"
	RolloutPhaseExporting       RolloutPhase = "exporting"
)

type PhaseStatus string

const (
	PhaseStatusStarted   PhaseStatus = "started"
	PhaseStatusCompleted PhaseStatus = "completed"
	PhaseStatusFailed    PhaseStatus = "failed"
)

type RolloutErrorCode string

const (
	RolloutErrorInputInvalid              RolloutErrorCode = "INPUT_INVALID"
	RolloutErrorInputResolutionFailed     RolloutErrorCode = "INPUT_RESOLUTION_FAILED"
	RolloutErrorTaskRuntimeSourceMissing  RolloutErrorCode = "TASK_RUNTIME_SOURCE_MISSING"
	RolloutErrorRuntimeImagePrepareFailed RolloutErrorCode = "RUNTIME_IMAGE_PREPARE_FAILED"
	RolloutErrorSandboxCreateFailed       RolloutErrorCode = "SANDBOX_CREATE_FAILED"
	RolloutErrorAgentFailed               RolloutErrorCode = "AGENT_FAILED"
	RolloutErrorAgentTimeout              RolloutErrorCode = "AGENT_TIMEOUT"
	RolloutErrorVerifierFailed            RolloutErrorCode = "VERIFIER_FAILED"
	RolloutErrorVerifierTimeout           RolloutErrorCode = "VERIFIER_TIMEOUT"
	RolloutErrorArtifactCaptureFailed     RolloutErrorCode = "ARTIFACT_CAPTURE_FAILED"
	RolloutErrorValidationFailed          RolloutErrorCode = "VALIDATION_FAILED"
	RolloutErrorExportNotReady            RolloutErrorCode = "EXPORT_NOT_READY"
	RolloutErrorInfrastructureFailure     RolloutErrorCode = "INFRA_FAILURE"
)

type RolloutError struct {
	Code      RolloutErrorCode  `json:"code"`
	Message   string            `json:"message"`
	Phase     RolloutPhase      `json:"phase"`
	Component string            `json:"component,omitempty"`
	Retriable bool              `json:"retriable"`
	Evidence  map[string]string `json:"evidence,omitempty"`
}

func (e RolloutError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

type PhaseEvent struct {
	RunID        string            `json:"run_id"`
	Phase        RolloutPhase      `json:"phase"`
	Status       PhaseStatus       `json:"status"`
	EpisodeID    string            `json:"episode_id,omitempty"`
	TaskID       string            `json:"task_id,omitempty"`
	AttemptIndex int               `json:"attempt_index,omitempty"`
	DurationMS   int64             `json:"duration_ms,omitempty"`
	Evidence     map[string]string `json:"evidence,omitempty"`
	Error        *RolloutError     `json:"error,omitempty"`
}

type PhaseReporter func(PhaseEvent)

type ArtifactManifestStatus string

const (
	ArtifactManifestStatusPresent ArtifactManifestStatus = "present"
	ArtifactManifestStatusMissing ArtifactManifestStatus = "missing"
	ArtifactManifestStatusFailed  ArtifactManifestStatus = "failed"
)

type ArtifactManifest struct {
	SchemaVersion string                  `json:"schema_version,omitempty"`
	EpisodeID     string                  `json:"episode_id"`
	GeneratedAt   time.Time               `json:"generated_at"`
	Entries       []ArtifactManifestEntry `json:"entries"`
}

type ArtifactManifestEntry struct {
	Kind        ArtifactKind           `json:"kind,omitempty"`
	Source      string                 `json:"source,omitempty"`
	Path        string                 `json:"path"`
	Status      ArtifactManifestStatus `json:"status"`
	SHA256      string                 `json:"sha256,omitempty"`
	MediaType   string                 `json:"media_type,omitempty"`
	SizeBytes   int64                  `json:"size_bytes,omitempty"`
	Description string                 `json:"description,omitempty"`
	Producer    string                 `json:"producer,omitempty"`
	Role        ArtifactRole           `json:"role,omitempty"`
	Error       string                 `json:"error,omitempty"`
}
