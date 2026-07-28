package rollout

import (
	"testing"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
)

func validParams() CreateParams {
	return CreateParams{Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO, Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Agent: &rolloutv1.RolloutAgent{Name: "codex", Profile: "production", ApprovalPolicy: "never"}, Model: "openai/gpt-5", Execution: &rolloutv1.RolloutExecution{Concurrency: 4, Attempts: 2}}}
}

func TestValidateCreateRequiresImmutableTaskSet(t *testing.T) {
	params := validParams()
	params.Spec.TaskSetRef = "registry.example/tasks/demo:latest"
	if err := ValidateCreate(params); err == nil {
		t.Fatal("mutable TaskSet tag was accepted")
	}
}
func TestValidateCreateRequiresManagedProfile(t *testing.T) {
	params := validParams()
	params.Spec.Agent.Profile = ""
	if err := ValidateCreate(params); err == nil {
		t.Fatal("managed rollout without profile was accepted")
	}
}
func TestValidateCreateRejectsUnmeteredStrictBudget(t *testing.T) {
	params := validParams()
	params.Spec.Agent = &rolloutv1.RolloutAgent{Name: "command", Command: "true"}
	params.Spec.Budget = &rolloutv1.RolloutBudget{MaxTokens: 1}
	if err := ValidateCreate(params); err == nil {
		t.Fatal("unmetered command agent token budget was accepted")
	}
}
func TestSpecHashIsMapOrderIndependent(t *testing.T) {
	first := validParams()
	first.Labels = map[string]string{"b": "2", "a": "1"}
	second := validParams()
	second.Labels = map[string]string{"a": "1", "b": "2"}
	a, err := SpecHash(first.Namespace, first.Spec, first.Labels, first.StartPolicy)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SpecHash(second.Namespace, second.Spec, second.Labels, second.StartPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("deterministic hashes differ: %s != %s", a, b)
	}
}
