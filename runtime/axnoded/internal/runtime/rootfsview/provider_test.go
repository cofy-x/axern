package rootfsview

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOverlayViewForContainerUsesFilestore(t *testing.T) {
	view := overlayViewForContainer("alloc-1", "/var/lib/axnoded/filestore", []string{"/image/rootfs"})

	assert.Equal(t, []string{"/image/rootfs"}, view.LowerDirs)
	assert.Equal(t, "/var/lib/axnoded/filestore/rootfs-views/alloc-1/upper", view.UpperDir)
	assert.Equal(t, "/var/lib/axnoded/filestore/rootfs-views/alloc-1/work", view.WorkDir)
	assert.Equal(t, "/var/lib/axnoded/filestore/rootfs-views/alloc-1/merged", view.MergedDir)
}
