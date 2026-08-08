package oci

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseHostDockerInternalEntries(t *testing.T) {
	content := `
192.0.2.10 host.docker.internal another-alias
192.0.2.10 host.docker.internal
2001:db8::10 another-alias host.docker.internal # retained alias
192.0.2.11 evilhost.docker.internal
not-an-ip host.docker.internal
192.0.2.12 localhost # host.docker.internal
`
	assert.Equal(t, []string{
		"192.0.2.10 host.docker.internal",
		"2001:db8::10 host.docker.internal",
	}, parseHostDockerInternalEntries(content))
}
