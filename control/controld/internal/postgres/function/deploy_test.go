package pgfunction

import (
	"testing"

	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
)

func TestDeletedFunctionDeployCreatesRevisionAndWorker(t *testing.T) {
	current := &functionv1.Function{Status: functionv1.FunctionStatus_FUNCTION_STATUS_DELETED}
	revision := &functionv1.FunctionRevision{SourceDigest: "source", ManifestDigest: "manifest"}
	deployment := &functionv1.FunctionDeployment{WorkerServiceID: "svc-deleted"}

	if deployIsCurrent(current, revision, "source", "manifest") {
		t.Fatal("deleted function must not be treated as the current deployment")
	}
	if got := reusableWorkerServiceID(current, deployment); got != "" {
		t.Fatalf("reusableWorkerServiceID() = %q, want empty", got)
	}
}

func TestActiveFunctionDeployReusesIdenticalRevisionAndWorker(t *testing.T) {
	current := &functionv1.Function{Status: functionv1.FunctionStatus_FUNCTION_STATUS_READY}
	revision := &functionv1.FunctionRevision{SourceDigest: "source", ManifestDigest: "manifest"}
	deployment := &functionv1.FunctionDeployment{WorkerServiceID: "svc-current"}

	if !deployIsCurrent(current, revision, "source", "manifest") {
		t.Fatal("active identical function should be treated as the current deployment")
	}
	if got := reusableWorkerServiceID(current, deployment); got != "svc-current" {
		t.Fatalf("reusableWorkerServiceID() = %q", got)
	}
}
