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
	allocated, err := directoryAllocatedSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if allocated >= logicalSize/2 {
		t.Fatalf("allocated size = %d, counted sparse logical capacity %d", allocated, logicalSize)
	}
}

func TestDirectoryAllocatedSizeAllowsMissingRoot(t *testing.T) {
	got, err := directoryAllocatedSize(filepath.Join(t.TempDir(), "missing"))
	if err != nil || got != 0 {
		t.Fatalf("directoryAllocatedSize() = (%d, %v), want (0, nil)", got, err)
	}
}

func TestStatusJSONNamesAllocatedDiskContract(t *testing.T) {
	data, err := json.Marshal(Status{DiskAllocatedBytes: 42})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"disk_allocated_bytes":42`) || strings.Contains(text, `"disk_bytes"`) {
		t.Fatalf("status JSON = %s", text)
	}
}
