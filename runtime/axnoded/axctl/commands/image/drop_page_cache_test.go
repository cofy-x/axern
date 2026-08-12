package image

import (
	"os"
	"testing"
)

func TestDropPageCacheRequestValidation(t *testing.T) {
	pageSize := int64(os.Getpagesize())
	valid := dropPageCacheRequest{
		ImageRef:      "registry.example/fixture@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ContainerPath: "/qualification/lower-payload.bin",
		LengthBytes:   32 << 20,
	}
	valid.LengthBytes = 8192 * pageSize
	if err := valid.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}

	for name, mutate := range map[string]func(*dropPageCacheRequest){
		"missing ref":     func(request *dropPageCacheRequest) { request.ImageRef = "" },
		"relative path":   func(request *dropPageCacheRequest) { request.ContainerPath = "qualification/file" },
		"unclean path":    func(request *dropPageCacheRequest) { request.ContainerPath = "/qualification/../file" },
		"root path":       func(request *dropPageCacheRequest) { request.ContainerPath = "/" },
		"negative offset": func(request *dropPageCacheRequest) { request.OffsetBytes = -1 },
		"zero length":     func(request *dropPageCacheRequest) { request.LengthBytes = 0 },
		"oversized":       func(request *dropPageCacheRequest) { request.LengthBytes = maxPageCacheDropBytes + pageSize },
		"unaligned":       func(request *dropPageCacheRequest) { request.LengthBytes = pageSize + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if err := request.validate(); err == nil {
				t.Fatal("validate() error = nil, want error")
			}
		})
	}
}

func TestUniqueMountedImageMatchesAnyExactIdentity(t *testing.T) {
	inventory := &inventoryResponse{MountedImages: []mountedImageDetail{
		{ImageURL: "registry.example/oci@sha256:aaa", CacheKey: "oci-cache", MountType: "oci", MountPath: "/oci"},
		{ImageURL: "registry.example/base@sha256:bbb", NydusImageURL: "registry.example/nydus@sha256:ccc", MountType: "nydus", MountPath: "/nydus"},
	}}
	for _, ref := range []string{
		"registry.example/oci@sha256:aaa",
		"oci-cache",
		"registry.example/nydus@sha256:ccc",
	} {
		mount, err := uniqueMountedImage(inventory, ref)
		if err != nil {
			t.Fatalf("uniqueMountedImage(%q) error = %v", ref, err)
		}
		if mount.MountPath == "" {
			t.Fatalf("uniqueMountedImage(%q) has no mount path", ref)
		}
	}
}

func TestUniqueMountedImageRejectsMissingAndDuplicate(t *testing.T) {
	inventory := &inventoryResponse{MountedImages: []mountedImageDetail{
		{ImageURL: "duplicate", MountType: "oci", MountPath: "/one"},
		{CacheKey: "duplicate", MountType: "oci", MountPath: "/two"},
	}}
	for _, ref := range []string{"missing", "duplicate"} {
		if _, err := uniqueMountedImage(inventory, ref); err == nil {
			t.Fatalf("uniqueMountedImage(%q) error = nil, want error", ref)
		}
	}
}

func TestUniqueMountedImageRejectsIncompleteInventoryRecord(t *testing.T) {
	for _, mount := range []mountedImageDetail{
		{ImageURL: "incomplete", MountType: "oci"},
		{ImageURL: "incomplete", MountPath: "/mount"},
	} {
		if _, err := uniqueMountedImage(&inventoryResponse{MountedImages: []mountedImageDetail{mount}}, "incomplete"); err == nil {
			t.Fatal("uniqueMountedImage() accepted incomplete mount inventory")
		}
	}
}
