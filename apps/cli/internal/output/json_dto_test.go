package output

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEnvironmentJSONUsesStableShape(t *testing.T) {
	var b strings.Builder
	err := PrintEnvironmentResponseJSON(&b, &environmentv1.Environment{
		ID:        "env-1",
		Namespace: "default",
		Spec: &environmentv1.EnvironmentSpec{Image: &environmentv1.EnvironmentImageSource{
			Ref: "docker.io/library/python:3.12-slim",
		}},
		CreatedAt: timestamppb.New(time.Date(
			2026, time.April, 29, 12, 0, 0, 0, time.UTC,
		)),
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{ImageDefaultArgv: []string{"python3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Environment struct {
			CreatedAt string `json:"created_at"`
			Spec      struct {
				Image struct {
					Ref string `json:"ref"`
				} `json:"image"`
			} `json:"spec"`
			ResolvedSpec struct {
				ImageDefaultArgv []string `json:"image_default_argv"`
			} `json:"resolved_spec"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Environment.CreatedAt != "2026-04-29T12:00:00Z" || got.Environment.Spec.Image.Ref != "docker.io/library/python:3.12-slim" {
		t.Fatalf("environment JSON = %#v, want stable labels and timestamps", got.Environment)
	}
	if len(got.Environment.ResolvedSpec.ImageDefaultArgv) != 1 || got.Environment.ResolvedSpec.ImageDefaultArgv[0] != "python3" || strings.Contains(b.String(), "bootstrap_argv") {
		t.Fatalf("environment JSON should expose image_default_argv and not bootstrap_argv: %s", b.String())
	}
	assertNoProtoJSONLeak(t, b.String())
}

func TestRunJSONUsesStableShape(t *testing.T) {
	createdAt := timestamppb.New(time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC))
	run := &runv1.Run{
		ID:                      "run-1",
		EnvironmentID:           "env-1",
		EnvironmentSpec:         &environmentv1.EnvironmentSpec{Image: &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/python:3.12-slim"}},
		ResolvedEnvironmentSpec: &environmentv1.ResolvedEnvironmentSpec{ImageDescriptor: &environmentv1.OciImageDescriptor{Digest: "sha256:frozen"}},
		Status:                  runv1.RunStatus_RUN_STATUS_RUNNING,
		Config: &commonv1.ExecutionConfig{
			Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT},
		},
		CreatedAt: createdAt,
		CapabilityConditions: &capabilityv1.CapabilityConditionSet{ObservedAt: createdAt, Conditions: []*capabilityv1.CapabilityCondition{{
			Key:        &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT}},
			State:      capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		}}},
		ExitCode: func() *int32 { value := int32(0); return &value }(),
	}
	var runJSON strings.Builder
	if err := PrintRunResponseJSON(&runJSON, run); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(runJSON.String(), "RUN_STATUS") || strings.Contains(runJSON.String(), `"seconds"`) || strings.Contains(runJSON.String(), "NETWORK_MODE") {
		t.Fatalf("run JSON leaked protobuf internals: %s", runJSON.String())
	}
	if !strings.Contains(runJSON.String(), `"exit_code": 0`) {
		t.Fatalf("run JSON should preserve known zero exit code: %s", runJSON.String())
	}
	if !strings.Contains(runJSON.String(), `"platform": "runsc_memory_hard_limit"`) || !strings.Contains(runJSON.String(), `"observed_at": "2026-04-29T12:00:00Z"`) {
		t.Fatalf("run JSON omitted structured capability condition: %s", runJSON.String())
	}
	if !strings.Contains(runJSON.String(), `"ref": "docker.io/library/python:3.12-slim"`) || !strings.Contains(runJSON.String(), `"digest": "sha256:frozen"`) {
		t.Fatalf("run JSON omitted frozen Environment input: %s", runJSON.String())
	}

	var failedRunJSON strings.Builder
	err := PrintRunResponseJSON(&failedRunJSON, &runv1.Run{
		ID:             "run-quota",
		EnvironmentID:  "env-1",
		Status:         runv1.RunStatus_RUN_STATUS_FAILED,
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED,
		Message:        "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	var failedRun struct {
		Run struct {
			DiagnosticCode string `json:"diagnostic_code"`
		} `json:"run"`
	}
	if err := json.Unmarshal([]byte(failedRunJSON.String()), &failedRun); err != nil {
		t.Fatal(err)
	}
	if failedRun.Run.DiagnosticCode != "admission-blocked" {
		t.Fatalf("failed run JSON = %#v, want typed admission diagnostic", failedRun.Run)
	}
}

func TestSecretJSONUsesStableShape(t *testing.T) {
	createdAt := timestamppb.New(time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC))
	var secretJSON strings.Builder
	if err := PrintSecretResponseJSON(&secretJSON, &secretv1.Secret{
		ID:        "sec-1",
		Namespace: "default",
		Type:      secretv1.SecretType_SECRET_TYPE_OPAQUE,
		DataKeys:  []string{"TOKEN"},
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(secretJSON.String(), "SECRET_TYPE") || strings.Contains(secretJSON.String(), `"seconds"`) {
		t.Fatalf("secret JSON leaked protobuf internals: %s", secretJSON.String())
	}
}

func assertNoProtoJSONLeak(t *testing.T, value string) {
	t.Helper()
	for _, unexpected := range []string{"STATUS_", `"seconds"`, `"nanos"`} {
		if strings.Contains(value, unexpected) {
			t.Fatalf("JSON leaked protobuf internals %q: %s", unexpected, value)
		}
	}
}
