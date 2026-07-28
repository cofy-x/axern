package nodestate

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func TestDBRecordLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "metadata.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	want := &apipb.Map{Items: map[string]string{"a": "one"}}
	if err := db.PutRecord("records", "allocation-a", want); err != nil {
		t.Fatalf("PutRecord() error = %v", err)
	}
	var got apipb.Map
	if err := db.GetRecord("records", "allocation-a", &got); err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if !reflect.DeepEqual(got.GetItems(), want.GetItems()) {
		t.Fatalf("GetRecord() = %#v, want %#v", got.GetItems(), want.GetItems())
	}
	if err := db.DeleteRecord("records", "allocation-a"); err != nil {
		t.Fatalf("DeleteRecord() error = %v", err)
	}
	if err := db.GetRecord("records", "allocation-a", &got); !errord.IsNotFound(err) {
		t.Fatalf("GetRecord() after delete error = %v, want not found", err)
	}
}

func TestDBSnapshotAndIterationSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := db.SaveSnapshot("network_interfaces", &apipb.Slice{Items: []string{"one"}}); err != nil {
		t.Fatalf("SaveSnapshot() error = %v", err)
	}
	for _, key := range []string{"b", "a"} {
		if err := db.PutRecord("allocations", key, &apipb.Map{Items: map[string]string{"id": key}}); err != nil {
			t.Fatalf("PutRecord(%q) error = %v", key, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	db, err = Open(path)
	if err != nil {
		t.Fatalf("Open() after close error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var snapshot apipb.Slice
	if err := db.LoadSnapshot("network_interfaces", &snapshot); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(snapshot.GetItems(), []string{"one"}) {
		t.Fatalf("LoadSnapshot() = %#v", snapshot.GetItems())
	}
	var keys []string
	if err := db.ForEachRecord("allocations", func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		t.Fatalf("ForEachRecord() error = %v", err)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Fatalf("ForEachRecord() keys = %#v", keys)
	}
}

func TestDBUsesPrivateFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("database permissions = %o, want 600", got)
	}
}

func TestDBForEachRecordAllowsRecordDeletion(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, key := range []string{"orphan-a", "orphan-b"} {
		if err := db.PutRecord("allocations", key, &apipb.Map{Items: map[string]string{"id": key}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ForEachRecord("allocations", func(key string, _ []byte) error {
		return db.DeleteRecord("allocations", key)
	}); err != nil {
		t.Fatalf("ForEachRecord() delete error = %v", err)
	}
	seen := 0
	if err := db.ForEachRecord("allocations", func(_ string, _ []byte) error {
		seen++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Fatalf("records after delete = %d, want 0", seen)
	}
}

func TestDBConcurrentRecordsRemainIsolated(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const records = 64
	var wait sync.WaitGroup
	for index := 0; index < records; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			key := fmt.Sprintf("allocation-%03d", index)
			if err := db.PutRecord("allocations", key, &apipb.Map{Items: map[string]string{"id": key}}); err != nil {
				t.Errorf("PutRecord(%q) error = %v", key, err)
			}
		}(index)
	}
	wait.Wait()
	seen := 0
	if err := db.ForEachRecord("allocations", func(_ string, _ []byte) error {
		seen++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen != records {
		t.Fatalf("records = %d, want %d", seen, records)
	}
}

func BenchmarkDBPutRecord(b *testing.B) {
	db, err := Open(filepath.Join(b.TempDir(), "metadata.db"))
	if err != nil {
		b.Fatalf("Open() error = %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	value := &apipb.Map{Items: map[string]string{"runtime": "runsc", "image": "registry.example/image@sha256:digest"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.PutRecord("allocations", "allocation", value); err != nil {
			b.Fatal(err)
		}
	}
}
