package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncAssetsDownloadsFixedFiles(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/@xterm/xterm/-/xterm-1.2.3.tgz":
			_, _ = w.Write(tarball(t, map[string]string{
				"package/lib/xterm.js":  "xterm js",
				"package/css/xterm.css": "xterm css",
			}))
		case "/@xterm/addon-fit/-/addon-fit-4.5.6.tgz":
			_, _ = w.Write(tarball(t, map[string]string{
				"package/lib/addon-fit.js": "fit js",
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	err := syncAssets(context.Background(), server.Client(), server.URL, dir, "1.2.3", "4.5.6")
	if err != nil {
		t.Fatalf("syncAssets() error = %v", err)
	}

	assertFile(t, filepath.Join(dir, "xterm.js"), "xterm js")
	assertFile(t, filepath.Join(dir, "xterm.css"), "xterm css")
	assertFile(t, filepath.Join(dir, "addon-fit.js"), "fit js")
	if len(requested) != 3 {
		t.Fatalf("request count = %d, want 3: %#v", len(requested), requested)
	}
}

func TestExtractTarballFileReportsMissingAsset(t *testing.T) {
	_, err := extractTarballFile(bytes.NewReader(tarball(t, map[string]string{
		"package/other.js": "other",
	})), "package/lib/xterm.js")
	if err == nil {
		t.Fatal("extractTarballFile() error = nil, want missing asset error")
	}
}

func assertFile(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
