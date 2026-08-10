package runtime

import (
	"testing"

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
