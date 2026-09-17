package parse

import "testing"

func TestImageMountsExpressReadonlyAsAnInvariant(t *testing.T) {
	mounts, err := ImageMounts([]string{"registry.example/tool:latest:/__tool"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0].GetImage() != "registry.example/tool:latest" || mounts[0].GetTarget() != "/__tool" {
		t.Fatalf("mounts = %#v", mounts)
	}
	if _, err := ImageMounts([]string{"registry.example/tool:latest:/__tool:ro"}); err == nil {
		t.Fatal("ImageMounts accepted obsolete :ro option")
	}
}
