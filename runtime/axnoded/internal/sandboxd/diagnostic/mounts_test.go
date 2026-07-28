package diagnostic

import "testing"

func TestParseMountInfoLine(t *testing.T) {
	line := `36 25 0:32 / /mnt rw,relatime - overlay overlay rw,lowerdir=/a:/b`
	mount, ok := parseMountInfoLine(line)
	if !ok {
		t.Fatal("parseMountInfoLine() ok = false")
	}
	if mount.Mountpoint != "/mnt" || mount.FSType != "overlay" || mount.Source != "overlay" {
		t.Fatalf("mount = %#v", mount)
	}
}

func TestMountsIncludesRequestedPath(t *testing.T) {
	snapshot := Mounts("/")
	if len(snapshot.Paths) != 1 || snapshot.Paths[0].Path != "/" || !snapshot.Paths[0].Exists {
		t.Fatalf("snapshot paths = %#v", snapshot.Paths)
	}
}
