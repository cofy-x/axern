package imagefsd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemon_SaveAndLoadMeta(t *testing.T) {
	tmpDir := t.TempDir()
	savedPath := filepath.Join(tmpDir, "daemon.json")

	originalMeta := DaemonMeta{
		ID:            "test-daemon-id",
		Name:          "test-daemon",
		MountPoint:    "/tmp/mnt",
		DaemonDir:     "/tmp/daemon",
		DaemonLogPath: "/tmp/daemon/log",
		PidFilePath:   "/tmp/daemon/pid",
		CfgPath:       "/tmp/daemon/cfg",
		CachePath:     "/tmp/daemon/cache",
		ImageMetaDir:  "/tmp/meta",
		ChunkDBDir:    "/tmp/chunkdb",
		SourceType:    "oss",
	}

	d := &Daemon{
		meta:      originalMeta,
		savedPath: savedPath,
	}

	if err := d.saveMeta(); err != nil {
		t.Fatalf("saveMeta() failed: %v", err)
	}
	if _, err := os.Stat(savedPath); os.IsNotExist(err) {
		t.Fatal("Meta file was not created")
	}

	loadedMeta := DaemonMeta{}
	file, err := os.Open(savedPath)
	if err != nil {
		t.Fatalf("Failed to open saved file: %v", err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&loadedMeta); err != nil {
		t.Fatalf("Failed to decode meta: %v", err)
	}

	if loadedMeta.ID != originalMeta.ID {
		t.Errorf("ID = %s, want %s", loadedMeta.ID, originalMeta.ID)
	}
	if loadedMeta.Name != originalMeta.Name {
		t.Errorf("Name = %s, want %s", loadedMeta.Name, originalMeta.Name)
	}
	if loadedMeta.SourceType != originalMeta.SourceType {
		t.Errorf("SourceType = %s, want %s", loadedMeta.SourceType, originalMeta.SourceType)
	}
}

func TestDaemon_UpdateExpired(t *testing.T) {
	d := &Daemon{}

	before := time.Now()
	d.updateExpired()
	after := time.Now()

	expectedMin := before.Add(daemonExpiredPeriod).UnixNano()
	expectedMax := after.Add(daemonExpiredPeriod).UnixNano()

	if d.expiredAt < expectedMin || d.expiredAt > expectedMax {
		t.Errorf("expiredAt = %d, want between %d and %d", d.expiredAt, expectedMin, expectedMax)
	}
}
