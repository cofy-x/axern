package publicv1

import (
	"testing"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
)

func TestClassifyDiagnosisRequiresEveryEvidenceKindForEveryEpisode(t *testing.T) {
	episodes := []*rolloutv1.Episode{{ID: "episode-1", ExecutionGeneration: 2}, {ID: "episode-2", ExecutionGeneration: 2}}
	response := &rolloutv1.DiagnoseRolloutResponse{Rollout: &rolloutv1.Rollout{Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED}, Diagnosis: rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_HEALTHY}
	required := []string{"task", "episode", "trajectory", "agent", "verifier", "reward", "manifest"}
	for _, episode := range episodes {
		for _, kind := range required {
			response.Artifacts = append(response.Artifacts, &rolloutv1.Artifact{EpisodeID: episode.GetID(), ExecutionGeneration: 2, Kind: kind, Status: rolloutv1.ArtifactStatus_ARTIFACT_STATUS_PRESENT})
		}
	}
	classifyDiagnosis(response, episodes)
	if response.GetDiagnosis() != rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_HEALTHY {
		t.Fatalf("complete evidence diagnosis = %s", response.GetDiagnosis())
	}

	response.Artifacts = response.Artifacts[:len(response.Artifacts)-1]
	// A duplicate from another episode must not hide the missing manifest.
	response.Artifacts = append(response.Artifacts, &rolloutv1.Artifact{EpisodeID: "episode-1", ExecutionGeneration: 2, Kind: "manifest", Status: rolloutv1.ArtifactStatus_ARTIFACT_STATUS_PRESENT})
	classifyDiagnosis(response, episodes)
	if response.GetDiagnosis() != rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_INCOMPLETE_EVIDENCE {
		t.Fatalf("incomplete evidence diagnosis = %s", response.GetDiagnosis())
	}
}

func TestClassifyDiagnosisReportsCapacityWaitForPendingEpisodeInRunningRollout(t *testing.T) {
	response := &rolloutv1.DiagnoseRolloutResponse{Rollout: &rolloutv1.Rollout{Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_RUNNING}, Diagnosis: rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_HEALTHY}
	classifyDiagnosis(response, []*rolloutv1.Episode{{ID: "active", Status: rolloutv1.EpisodeStatus_EPISODE_STATUS_AGENT_RUNNING}, {ID: "waiting", Status: rolloutv1.EpisodeStatus_EPISODE_STATUS_PENDING}})
	if response.GetDiagnosis() != rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_CAPACITY_WAIT {
		t.Fatalf("diagnosis = %s", response.GetDiagnosis())
	}
}

func TestClassifyDiagnosisReportsPlanningInfrastructureFailure(t *testing.T) {
	response := &rolloutv1.DiagnoseRolloutResponse{Rollout: &rolloutv1.Rollout{Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED, Message: "planner failed"}, Diagnosis: rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_HEALTHY}
	classifyDiagnosis(response, nil)
	if response.GetDiagnosis() != rolloutv1.DiagnosisClass_DIAGNOSIS_CLASS_INFRASTRUCTURE_FAILURE {
		t.Fatalf("diagnosis = %s", response.GetDiagnosis())
	}
}
