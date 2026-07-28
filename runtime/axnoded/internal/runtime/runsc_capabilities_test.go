package runtime

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/stretchr/testify/assert"
)

func TestRunscRequirementsSkipCgroupWhenIgnored(t *testing.T) {
	handler := &RunscServiceHandler{ignoreCgroups: true}

	reqs := handler.Requirements()
	assert.False(t, reqs.NeedsCgroup)
	assert.Equal(t, []resourcemanager.ResourceName{
		resourcemanager.InterfaceResourceName,
	}, reqs.Resources)
}

func TestRunscEligibleForExecutionEnvelope(t *testing.T) {
	request := &runtimeapi.StartRequest{
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "runsc-envelope",
			Sandbox: config.RuntimeNameRunsc,
		},
	}
	if !(&RunscServiceHandler{}).EligibleForExecutionEnvelope(request) {
		t.Fatal("expected runsc static-only request to be envelope eligible")
	}

	request.UserEnvs = map[string]string{"FOO": "bar"}
	if (&RunscServiceHandler{}).EligibleForExecutionEnvelope(request) {
		t.Fatal("expected dynamic runsc request to be envelope ineligible")
	}
}
