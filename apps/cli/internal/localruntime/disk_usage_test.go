package localruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectoryAllocatedSizeDoesNotCountSparseCapacity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse.img")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	const logicalSize = int64(64 << 20)
	if err := file.Truncate(logicalSize); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{1}, logicalSize-1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	allocated, partial, err := directoryAllocatedSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if partial {
		t.Fatal("readable directory was reported as partial")
	}
	if allocated >= logicalSize/2 {
		t.Fatalf("allocated size = %d, counted sparse logical capacity %d", allocated, logicalSize)
	}
}

func TestDirectoryAllocatedSizeAllowsMissingRoot(t *testing.T) {
	got, partial, err := directoryAllocatedSize(filepath.Join(t.TempDir(), "missing"))
	if err != nil || got != 0 || partial {
		t.Fatalf("directoryAllocatedSize() = (%d, %t, %v), want (0, false, nil)", got, partial, err)
	}
}

func TestDirectoryAllocatedSizeSkipsUnreadableRuntimeState(t *testing.T) {
	dir := t.TempDir()
	private := filepath.Join(dir, "identity")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "node.pem"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(private, 0o700) })
	_, partial, err := directoryAllocatedSize(dir)
	if err != nil {
		t.Fatalf("root-owned runtime state made disk diagnostics fail: %v", err)
	}
	if !partial {
		t.Fatal("unreadable runtime state was not reported as partial")
	}
}

func TestStatusJSONNamesAllocatedDiskContract(t *testing.T) {
	data, err := json.Marshal(Status{DiskAllocatedBytes: 42, DiskUsagePartial: true})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"disk_allocated_bytes":42`) || !strings.Contains(text, `"disk_usage_partial":true`) || strings.Contains(text, `"disk_bytes"`) {
		t.Fatalf("status JSON = %s", text)
	}
}
