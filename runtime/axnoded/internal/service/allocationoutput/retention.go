package allocationoutput

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Retention owns sealed output bytes, never execution or admission authority.
// The manifest is published only after both bounded log files have been synced.
// The immutable control-plane expiry is also the durable deletion intent.
type Retention struct{ root string }

type manifest struct {
	AllocationID string    `json:"allocation_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Sources struct {
	Stdout   string
	Stderr   string
	Terminal bool
}

func NewRetention(root string) *Retention {
	return &Retention{root: filepath.Join(root, "allocation-output")}
}

func (r *Retention) path(id string) (string, error) {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, "/\\") {
		return "", fmt.Errorf("invalid Allocation output identity")
	}
	return filepath.Join(r.root, id), nil
}

func (r *Retention) Preserve(id string, expiry time.Time, source Sources) error {
	if !time.Now().Before(expiry) {
		return nil
	}
	dest, err := r.path(id)
	if err != nil {
		return err
	}
	if existing, err := r.readManifest(dest); err == nil {
		if existing.AllocationID != id || !existing.ExpiresAt.Equal(expiry) {
			return fmt.Errorf("Allocation output retention identity or expiry conflict")
		}
		for _, name := range []string{"stdout", "stderr"} {
			info, err := os.Stat(filepath.Join(dest, name))
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("retained output is not a regular file")
			}
		}
		return syncDir(r.root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(r.root, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(r.root, ".sealing-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for name, path := range map[string]string{"stdout": source.Stdout, "stderr": source.Stderr} {
		if err := copyOutput(path, filepath.Join(tmp, name)); err != nil {
			return err
		}
	}
	payload, err := json.Marshal(manifest{AllocationID: id, ExpiresAt: expiry.UTC()})
	if err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(tmp, "manifest.json"), payload); err != nil {
		return err
	}
	if err := syncDir(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	return syncDir(r.root)
}

func copyOutput(source, dest string) error {
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if source != "" {
		in, err := os.Open(source)
		if err != nil {
			return fmt.Errorf("open output for retention: %w", err)
		}
		defer in.Close()
		info, err := in.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("output source is not a regular file")
		}
		// Keep either stream's readable prefix plus one byte to preserve the
		// truncation signal. This also preserves every valid streaming cursor.
		if _, err := io.Copy(out, io.LimitReader(in, MaxOutputBytes+1)); err != nil {
			return err
		}
	}
	return out.Sync()
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (r *Retention) readManifest(path string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.ExpiresAt.IsZero() {
		return m, fmt.Errorf("output manifest has no expiry")
	}
	return m, nil
}

func (r *Retention) Sources(id string, now time.Time) (Sources, error) {
	path, err := r.path(id)
	if err != nil {
		return Sources{}, err
	}
	m, err := r.readManifest(path)
	if err != nil {
		return Sources{}, err
	}
	if m.AllocationID != id {
		return Sources{}, fmt.Errorf("output manifest identity mismatch")
	}
	if !now.Before(m.ExpiresAt) {
		return Sources{}, os.ErrNotExist
	}
	return Sources{Stdout: filepath.Join(path, "stdout"), Stderr: filepath.Join(path, "stderr"), Terminal: true}, nil
}

// Sweep only removes expired sealed output. Unfinished sealing directories are
// removed by Recover before any writer starts. Malformed records fail closed.
func (r *Retention) Sweep(now time.Time, recover bool) error {
	entries, err := os.ReadDir(r.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var result error
	for _, entry := range entries {
		path := filepath.Join(r.root, entry.Name())
		if strings.HasPrefix(entry.Name(), ".sealing-") {
			if recover {
				result = errors.Join(result, os.RemoveAll(path))
			}
			continue
		}
		m, err := r.readManifest(path)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if m.AllocationID != entry.Name() {
			result = errors.Join(result, fmt.Errorf("output manifest identity mismatch"))
			continue
		}
		if !now.Before(m.ExpiresAt) {
			result = errors.Join(result, os.RemoveAll(path))
		}
	}
	return result
}
