//go:build linux

package rootfsview

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMountInfoLineReadsOverlayLowerdir(t *testing.T) {
	line := `216 214 0:59 / /var/lib/imagemgr/oci/mounts/nginx/merged ro,relatime - overlay overlay ro,lowerdir=/lower/c:/lower/b:/lower/a,redirect_dir=on`

	info, ok := parseMountInfoLine(line)

	assert.True(t, ok)
	assert.Equal(t, "/var/lib/imagemgr/oci/mounts/nginx/merged", info.mountpoint)
	assert.Equal(t, "overlay", info.fsType)
	assert.Equal(t, "/lower/c:/lower/b:/lower/a", mountOptionValue(info.superOptions, "lowerdir"))
}

func TestUnescapeMountInfoPath(t *testing.T) {
	assert.Equal(t, "/tmp/root fs", unescapeMountInfoPath(`/tmp/root\040fs`))
}

func TestPathIsAtOrBelowRootMountpoint(t *testing.T) {
	assert.True(t, pathIsAtOrBelowMountpoint("/tmp/rootfs", "/"))
}
