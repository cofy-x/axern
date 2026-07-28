package verifyutil

import (
	"testing"
)

func TestBuildRootfsSpecLocal(t *testing.T) {
	cfg, err := BuildRootfsSpec("local", "/rootfs", "", "", "", "", "", "")
	if err != nil {
		t.Fatalf("BuildRootfsSpec returned error: %v", err)
	}
	if cfg.Type != "local" || cfg.LocalRootfsPath != "/rootfs" {
		t.Fatalf("unexpected local rootfs spec: %#v", cfg)
	}
}

func TestBuildRootfsSpecImageRequiresURL(t *testing.T) {
	if _, err := BuildRootfsSpec("image", "", "", "", "", "", "", ""); err == nil {
		t.Fatal("BuildRootfsSpec should reject missing image URL")
	}
}

func TestBuildRootfsSpecS3(t *testing.T) {
	cfg, err := BuildRootfsSpec("s3", "", "", "oss.local", "bucket", "object", "ak", "sk")
	if err != nil {
		t.Fatalf("BuildRootfsSpec returned error: %v", err)
	}
	if cfg.Type != "s3" || cfg.S3Rootfs.GetBucket() != "bucket" {
		t.Fatalf("unexpected s3 rootfs spec: %#v", cfg)
	}
}
