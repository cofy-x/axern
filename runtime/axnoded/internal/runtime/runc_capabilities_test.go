package runtime

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/assert"
)

func TestRuncRequirementsSkipCgroupWhenIgnored(t *testing.T) {
	handler := &RuncServiceHandler{ignoreCgroups: true}

	reqs := handler.Requirements()
	assert.False(t, reqs.NeedsCgroup)
	assert.Equal(t, []resourcemanager.ResourceName{
		resourcemanager.InterfaceResourceName,
	}, reqs.Resources)
}

func TestRuncHandlerCapabilitiesConservative(t *testing.T) {
	assert.Equal(t, contract.RuntimeCapabilities{
		CanExecDirect: true,
	}, (&RuncServiceHandler{}).Capabilities())
}

func TestRuncEligibleForExecutionEnvelope(t *testing.T) {
	request := &runtimeapi.StartRequest{
		RuntimeTemplate: &runtimeapi.RuntimeTemplate{
			ID:      "runc-envelope",
			Sandbox: config.RuntimeNameRunc,
		},
	}
	if !(&RuncServiceHandler{}).EligibleForExecutionEnvelope(request) {
		t.Fatal("expected runc static-only request to be envelope eligible")
	}

	request.Stdout = "/tmp/stdout.log"
	if (&RuncServiceHandler{}).EligibleForExecutionEnvelope(request) {
		t.Fatal("expected explicit stdio to disable runc envelope eligibility")
	}
}
