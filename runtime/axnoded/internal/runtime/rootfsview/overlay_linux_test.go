//go:build linux

package rootfsview

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMountInfoLineReadsOverlayLowerdir(t *testing.T) {
	line := `216 214 0:59 / /var/lib/imagemgr/oci/mounts/nginx/merged ro,relatime - overlay overlay ro,lowerdir=/lower/c:/lower/b:/lower/a,redirect_dir=on`

	info, ok := parseMountInfoLine(line)

	assert.True(t, ok)
	assert.Equal(t, "/var/lib/imagemgr/oci/mounts/nginx/merged", info.mountpoint)
	assert.Equal(t, "overlay", info.fsType)
	assert.Equal(t, 216, info.mountID)
	assert.Equal(t, "/", info.mountRoot)
	assert.True(t, info.mountOptions == "ro,relatime")
	assert.Equal(t, "/lower/c:/lower/b:/lower/a", mountOptionValue(info.superOptions, "lowerdir"))
}

func TestParseMountInfoLineReadsEROFSBacking(t *testing.T) {
	line := `301 214 7:1 / /var/lib/imagemgr/erofs/python ro,relatime - erofs /dev/loop7 ro`
	info, ok := parseMountInfoLine(line)
	require.True(t, ok)
	assert.Equal(t, "erofs", info.fsType)
	assert.Equal(t, "/dev/loop7", info.source)
	assert.True(t, mountOptionsContain(info.mountOptions, "ro"))
}

func TestResolveOverlayLowerDirsAppliesMountRoot(t *testing.T) {
	info := mountInfoEntry{
		mountRoot: "/subtree", mountpoint: "/mnt/image", fsType: "overlay",
		superOptions: "lowerdir=/lower/b:/lower/a,upperdir=/upper",
	}
	got, err := resolveOverlayLowerDirsFromInfo("/mnt/image/bin", info)
	require.NoError(t, err)
	assert.Equal(t, []string{"/upper/subtree/bin", "/lower/b/subtree/bin", "/lower/a/subtree/bin"}, got)
}

func TestResolveOverlayLowerDirsRejectsEscapedOption(t *testing.T) {
	_, err := resolveOverlayLowerDirsFromInfo("/mnt/image", mountInfoEntry{
		mountRoot: "/", mountpoint: "/mnt/image", fsType: "overlay", superOptions: `lowerdir=/lower\072name`,
	})
	require.ErrorContains(t, err, "unsupported mount-option escaping")
}

func TestUnescapeMountInfoPath(t *testing.T) {
	assert.Equal(t, "/tmp/root fs", unescapeMountInfoPath(`/tmp/root\040fs`))
}

func TestPathIsAtOrBelowRootMountpoint(t *testing.T) {
	assert.True(t, pathIsAtOrBelowMountpoint("/tmp/rootfs", "/"))
}
