package api

import (
	"testing"
)

func TestGenerateOSSID(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		bucket   string
		object   string
	}{
		{
			name:     "basic case",
			endpoint: "oss-cn-hangzhou.aliyuncs.com",
			bucket:   "my-bucket",
			object:   "images/test.tar",
		},
		{
			name:     "different values",
			endpoint: "oss-cn-beijing.aliyuncs.com",
			bucket:   "another-bucket",
			object:   "data/file.bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := generateOSSID(tt.endpoint, tt.bucket, tt.object)
			id2 := generateOSSID(tt.endpoint, tt.bucket, tt.object)

			// Same input should produce same ID
			if id1 != id2 {
				t.Errorf("generateOSSID() produced different IDs for same input: %s != %s", id1, id2)
			}

			// ID should be a valid hex string (SHA256 produces 64 hex chars)
			if len(id1) != 64 {
				t.Errorf("generateOSSID() produced ID of length %d, want 64", len(id1))
			}
		})
	}

	// Test that different inputs produce different IDs
	id1 := generateOSSID("endpoint1", "bucket1", "object1")
	id2 := generateOSSID("endpoint2", "bucket1", "object1")
	id3 := generateOSSID("endpoint1", "bucket2", "object1")
	id4 := generateOSSID("endpoint1", "bucket1", "object2")

	if id1 == id2 || id1 == id3 || id1 == id4 {
		t.Error("generateOSSID() produced same ID for different inputs")
	}
}

func TestGenerateNydusID(t *testing.T) {
	tests := []struct {
		name     string
		imageURL string
	}{
		{
			name:     "docker hub image",
			imageURL: "docker.io/library/alpine:latest",
		},
		{
			name:     "private registry",
			imageURL: "reg.example.com/namespace/image:v1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := generateNydusID(tt.imageURL)
			id2 := generateNydusID(tt.imageURL)

			// Same input should produce same ID
			if id1 != id2 {
				t.Errorf("generateNydusID() produced different IDs for same input: %s != %s", id1, id2)
			}

			// ID should be a valid hex string (SHA256 produces 64 hex chars)
			if len(id1) != 64 {
				t.Errorf("generateNydusID() produced ID of length %d, want 64", len(id1))
			}
		})
	}

	// Test that different images produce different IDs
	id1 := generateNydusID("docker.io/library/alpine:latest")
	id2 := generateNydusID("docker.io/library/ubuntu:latest")

	if id1 == id2 {
		t.Error("generateNydusID() produced same ID for different images")
	}

	// Test that Nydus IDs are different from OSS IDs with same string
	ossID := generateOSSID("", "", "docker.io/library/alpine:latest")
	nydusID := generateNydusID("docker.io/library/alpine:latest")

	if ossID == nydusID {
		t.Error("Nydus ID and OSS ID should be different due to 'nydus:' prefix")
	}
}

func TestOSSMountRequest_String(t *testing.T) {
	req := &OSSMountRequest{
		MountPoint: "/mnt/test",
		Endpoint:   "oss-cn-hangzhou.aliyuncs.com",
		Bucket:     "my-bucket",
		Object:     "images/test.tar",
	}

	str := req.String()
	expected := "(/mnt/test, oss-cn-hangzhou.aliyuncs.com, my-bucket, images/test.tar)"

	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestOSSUmountRequest_String(t *testing.T) {
	req := &OSSUmountRequest{
		Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		Bucket:   "my-bucket",
		Object:   "images/test.tar",
	}

	str := req.String()
	expected := "(oss-cn-hangzhou.aliyuncs.com my-bucket images/test.tar)"

	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestOCIMountRequest_String(t *testing.T) {
	req := &OCIMountRequest{
		ImageURL: "docker.io/library/alpine:latest",
	}

	str := req.String()
	expected := "(docker.io/library/alpine:latest)"

	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestOCIUmountRequest_String(t *testing.T) {
	req := &OCIUmountRequest{
		ImageURL: "docker.io/library/alpine:latest",
	}

	str := req.String()
	expected := "(docker.io/library/alpine:latest)"

	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestNydusMountRequest_String(t *testing.T) {
	req := &NydusMountRequest{
		ImageURL:   "docker.io/library/alpine:latest",
		MountPoint: "/mnt/nydus",
	}

	str := req.String()
	expected := "(docker.io/library/alpine:latest, /mnt/nydus)"

	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestNydusUmountRequest_String(t *testing.T) {
	req := &NydusUmountRequest{
		ImageURL: "docker.io/library/alpine:latest",
	}

	str := req.String()
	expected := "(docker.io/library/alpine:latest)"

	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestNewHttpWorker(t *testing.T) {
	worker, err := NewHttpWorker(&HttpWorkerConfig{
		NydusSuffix: "-nydus",
	})
	if err != nil {
		t.Fatalf("NewHttpWorker() error = %v", err)
	}

	if worker == nil {
		t.Fatal("NewHttpWorker() returned nil")
	}

	if worker.nydusSuffix != "-nydus" {
		t.Errorf("nydusSuffix = %q, want %q", worker.nydusSuffix, "-nydus")
	}

	if worker.nydusCache == nil {
		t.Error("nydusCache was not initialized")
	}
}

func TestHttpClient_NewHttpClient(t *testing.T) {
	tests := []struct {
		name         string
		sockPath     string
		expectedPath string
	}{
		{
			name:         "custom socket path",
			sockPath:     "/custom/path/socket.sock",
			expectedPath: "/custom/path/socket.sock",
		},
		{
			name:         "empty path defaults to default",
			sockPath:     "",
			expectedPath: DefaultHttpSockPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewHttpClient(tt.sockPath)
			if client == nil {
				t.Fatal("NewHttpClient() returned nil")
			}
			if client.clt == nil {
				t.Error("HTTP client was not initialized")
			}
		})
	}
}

// Benchmark tests for critical paths
func BenchmarkGenerateOSSID(b *testing.B) {
	endpoint := "oss-cn-hangzhou.aliyuncs.com"
	bucket := "my-bucket"
	object := "images/test.tar"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateOSSID(endpoint, bucket, object)
	}
}

func BenchmarkGenerateNydusID(b *testing.B) {
	imageURL := "docker.io/library/alpine:latest"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateNydusID(imageURL)
	}
}

func BenchmarkSplitObject(b *testing.B) {
	object := "path/to/images/myimage.tar"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = splitObject(object)
	}
}
