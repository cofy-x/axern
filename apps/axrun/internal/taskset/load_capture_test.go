package taskset

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractPayloadArchiveWritesRegularFiles(t *testing.T) {
	var data bytes.Buffer
	w := tar.NewWriter(&data)
	entries := []struct {
		name string
		body string
	}{
		{name: "tasks/example/workspace/main.py", body: "print('ok')\n"},
		{name: "tasks/example/verifier/test.py", body: "assert True\n"},
	}
	for _, entry := range entries {
		if err := w.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	destination := t.TempDir()
	if err := extractPayloadArchive(bytes.NewReader(data.Bytes()), destination); err != nil {
		t.Fatalf("extractPayloadArchive returned error: %v", err)
	}
	for _, entry := range entries {
		got, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(entry.name)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != entry.body {
			t.Fatalf("%s = %q, want %q", entry.name, got, entry.body)
		}
	}
}

func TestExtractPayloadArchiveRejectsUnsafeEntries(t *testing.T) {
	tests := []tar.Header{
		{Name: "../escape", Mode: 0o644, Typeflag: tar.TypeReg},
		{Name: "/absolute", Mode: 0o644, Typeflag: tar.TypeReg},
		{Name: "tasks/link", Linkname: "elsewhere", Typeflag: tar.TypeSymlink},
		{Name: "tasks/hardlink", Linkname: "tasks/file", Typeflag: tar.TypeLink},
	}
	for _, header := range tests {
		header := header
		t.Run(strings.ReplaceAll(header.Name, "/", "_"), func(t *testing.T) {
			var data bytes.Buffer
			w := tar.NewWriter(&data)
			if err := w.WriteHeader(&header); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if err := extractPayloadArchive(bytes.NewReader(data.Bytes()), t.TempDir()); err == nil {
				t.Fatalf("extractPayloadArchive accepted %#v", header)
			}
		})
	}
}
