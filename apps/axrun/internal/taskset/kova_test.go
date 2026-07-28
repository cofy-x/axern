package taskset

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKovaBuildArchiveIsMinimalAndStable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.tar"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := kovaBuildArchive(root, "registry.local/demo:payload")
	if err != nil {
		t.Fatal(err)
	}
	b, err := kovaBuildArchive(root, "registry.local/demo:payload")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("Kova build archive is not deterministic")
	}
	reader, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"taskset/Dockerfile", "taskset/metadata.json", "taskset/payload.tar"}
	if len(reader.File) != len(want) {
		t.Fatalf("files=%d", len(reader.File))
	}
	for index, file := range reader.File {
		if file.Name != want[index] {
			t.Fatalf("file[%d]=%q", index, file.Name)
		}
	}
}

func TestKovaPublisherCancelsJobWithContext(t *testing.T) {
	cancelled := make(chan struct{}, 1)
	created := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/builds":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"job-cancel","status":"queued"}`))
			created <- struct{}{}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/builds/job-cancel/cancel":
			cancelled <- struct{}{}
			_, _ = w.Write([]byte(`{"id":"job-cancel","status":"cancelled"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.tar"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		resolved := Resolved{
			DescriptorPath: filepath.Join(root, "descriptor.json"),
			Descriptor: Descriptor{
				SourceDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			},
		}
		_, err := (kovaPublisher{endpoint: server.URL}).Publish(ctx, resolved, "registry.local/demo:payload")
		done <- err
	}()
	select {
	case <-created:
		time.Sleep(20 * time.Millisecond)
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("Kova build was not submitted")
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
			t.Fatalf("publish error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publisher did not stop")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Kova cancel was not called")
	}
}

func TestKovaPublisherUsesTypedResults(t *testing.T) {
	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/builds":
			creates.Add(1)
			_ = r.ParseMultipartForm(1 << 20)
			if r.FormValue("formats") != "oci,nydus" || r.FormValue("source_digest") == "" {
				t.Errorf("form=%v", r.MultipartForm.Value)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"job-1","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/builds/job-1":
			_, _ = w.Write([]byte(`{"id":"job-1","status":"succeeded"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/builds/job-1/results":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
				{"format": "nydus", "status": "succeeded", "repository": "registry.local/demo:payload_nydus_v3", "manifest_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "media_type": "application/vnd.oci.image.manifest.v1+json", "size": 100},
				{"format": "oci", "status": "succeeded", "repository": "registry.local/demo:payload", "manifest_digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "media_type": "application/vnd.oci.image.manifest.v1+json", "size": 200},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload.tar"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := Resolved{
		DescriptorPath: filepath.Join(root, "descriptor.json"),
		Descriptor: Descriptor{
			SourceDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		},
	}
	published, err := (kovaPublisher{endpoint: server.URL}).Publish(context.Background(), resolved, "registry.local/demo:payload")
	if err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 1 || published.BuildID != "job-1" || len(published.Payloads) != 2 || published.Payloads[0].Format != "nydus" {
		t.Fatalf("published=%#v", published)
	}
}
