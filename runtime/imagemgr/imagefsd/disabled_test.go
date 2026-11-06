package imagefsd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDisabledPreservesEveryDaemonRecord(t *testing.T) {
	for _, data := range []string{`{"source_type":"nydus"}`, `{"source_type":"unknown"}`, `{`} {
		t.Run(data, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "daemon_configs")
			if err := os.Mkdir(dir, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "daemon.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := ValidateDisabled(root); err == nil {
				t.Fatal("accepted retained daemon authority")
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, []byte(data)) {
				t.Fatalf("retained daemon authority changed: %v", err)
			}
		})
	}
}

func TestValidateDisabledDoesNotCreateOrIgnoreDaemonState(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "daemon_configs")
	if err := ValidateDisabled(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("validation created metadata directory: %v", err)
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDisabled(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "unexpected-directory"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDisabled(root); err == nil {
		t.Fatal("ignored unrecognized daemon state")
	}
}
