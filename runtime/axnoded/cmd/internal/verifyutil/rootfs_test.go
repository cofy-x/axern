package verifyutil

import (
	"testing"
)

func TestBuildRootfsSpecLocal(t *testing.T) {
	cfg, err := BuildRootfsSpec("local", "/rootfs", "")
	if err != nil {
		t.Fatalf("BuildRootfsSpec returned error: %v", err)
	}
	if cfg.Type != "local" || cfg.LocalRootfsPath != "/rootfs" {
		t.Fatalf("unexpected local rootfs spec: %#v", cfg)
	}
}

func TestBuildRootfsSpecImageRequiresURL(t *testing.T) {
	if _, err := BuildRootfsSpec("image", "", ""); err == nil {
		t.Fatal("BuildRootfsSpec should reject missing image URL")
	}
}

func TestBuildRootfsSpecRejectsRemovedS3(t *testing.T) {
	if _, err := BuildRootfsSpec("s3", "", ""); err == nil {
		t.Fatal("removed S3 rootfs source must be rejected")
	}
}
