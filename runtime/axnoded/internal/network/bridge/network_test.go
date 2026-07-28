package bridge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDNATOutputRuleOnlyMatchesLocalDestinations(t *testing.T) {
	assert.Equal(t, []string{
		"-p", "tcp",
		"-m", "addrtype", "--dst-type", "LOCAL",
		"--dport", "8080",
		"-j", "DNAT", "--to-destination", "198.18.0.21:8080",
	}, dnatOutputRule("tcp", "8080", "198.18.0.21:8080"))
}
