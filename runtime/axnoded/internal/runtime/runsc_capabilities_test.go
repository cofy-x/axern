package runtime

import (
	"testing"

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
