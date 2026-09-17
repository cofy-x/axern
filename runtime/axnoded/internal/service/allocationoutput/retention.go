package allocationoutput

import (
	"context"
	"crypto/sha256"
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
// The manifest is published only after bounded logs and declared objects have
// been synced.
// The immutable control-plane expiry is also the durable deletion intent.
type Retention struct{ root string }

var ErrSealedStateUnavailable = errors.New("sealed output state is unavailable")

type Entry struct {
	OutputID  string    `json:"output_id"`
	Path      string    `json:"path"`
	Object    string    `json:"object,omitempty"`
	SHA256    string    `json:"sha256,omitempty"`
	MediaType string    `json:"media_type,omitempty"`
	Format    string    `json:"format"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
	SizeBytes int64     `json:"size_bytes,omitempty"`
	SealedAt  time.Time `json:"sealed_at"`
}

type Manifest struct {
	AllocationID string    `json:"allocation_id"`
	ExpiresAt    time.Time `json:"expires_at"`
	Entries      []Entry   `json:"entries,omitempty"`
}

type Capture func(context.Context, string) ([]Entry, error)

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
	return r.Seal(context.Background(), id, expiry, source, nil)
}

func (r *Retention) Seal(ctx context.Context, id string, expiry time.Time, source Sources, capture Capture) error {
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
		for _, entry := range existing.Entries {
			if entry.Status != "available" {
				continue
			}
			if entry.Object == "" || filepath.Base(entry.Object) != entry.Object || strings.ContainsAny(entry.Object, "/\\") {
				return fmt.Errorf("retained output object identity is invalid")
			}
			objectPath := filepath.Join(dest, "objects", entry.Object)
			info, err := os.Stat(objectPath)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || info.Size() != entry.SizeBytes {
				return fmt.Errorf("retained output object does not match manifest")
			}
			if entry.SHA256 == "" {
				return fmt.Errorf("retained output object has no integrity digest")
			}
			digest, err := fileSHA256(objectPath)
			if err != nil {
				return err
			}
			if digest != entry.SHA256 {
				return fmt.Errorf("retained output object digest does not match manifest")
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
	entries := []Entry(nil)
	if capture != nil {
		objects := filepath.Join(tmp, "objects")
		if err := os.Mkdir(objects, 0o700); err != nil {
			return err
		}
		entries, err = capture(ctx, objects)
		if err != nil {
			return err
		}
		if err := syncDir(objects); err != nil {
			return err
		}
	}
	payload, err := json.Marshal(Manifest{AllocationID: id, ExpiresAt: expiry.UTC(), Entries: entries})
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
	n, err := f.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return f.Sync()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// CreateObject creates one immutable object in a private sealing directory.
// The caller must close the returned file before publishing the manifest.
func CreateObject(objectsDir, objectID string) (*os.File, error) {
	if objectID == "" || filepath.Base(objectID) != objectID || strings.ContainsAny(objectID, "/\\") {
		return nil, fmt.Errorf("invalid sealed output object identity")
	}
	return os.OpenFile(filepath.Join(objectsDir, objectID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func (r *Retention) readManifest(path string) (Manifest, error) {
	var m Manifest
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

func (r *Retention) Manifest(id string, now time.Time) (Manifest, error) {
	path, err := r.path(id)
	if err != nil {
		return Manifest{}, err
	}
	m, err := r.readManifest(path)
	if err != nil {
		return Manifest{}, err
	}
	if m.AllocationID != id {
		return Manifest{}, fmt.Errorf("output manifest identity mismatch")
	}
	if !now.Before(m.ExpiresAt) {
		return Manifest{}, os.ErrNotExist
	}
	return m, nil
}

func (r *Retention) ReadSealedOutput(id, outputID string, offset, limit int64, now time.Time) ([]byte, int64, bool, error) {
	if offset < 0 || limit <= 0 {
		return nil, offset, false, fmt.Errorf("sealed output offset or limit is invalid")
	}
	m, err := r.Manifest(id, now)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, offset, false, fmt.Errorf("%w: manifest is missing or expired", ErrSealedStateUnavailable)
		}
		return nil, offset, false, err
	}
	var selected *Entry
	for index := range m.Entries {
		if m.Entries[index].OutputID == outputID {
			selected = &m.Entries[index]
			break
		}
	}
	if selected == nil || selected.Status != "available" || selected.Object == "" {
		return nil, offset, false, os.ErrNotExist
	}
	if filepath.Base(selected.Object) != selected.Object || strings.ContainsAny(selected.Object, "/\\") {
		return nil, offset, false, fmt.Errorf("sealed output object identity is invalid")
	}
	objectPath := filepath.Join(r.root, id, "objects", selected.Object)
	file, err := os.Open(objectPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, offset, false, fmt.Errorf("%w: object is missing", ErrSealedStateUnavailable)
		}
		return nil, offset, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, offset, false, err
	}
	if !info.Mode().IsRegular() || info.Size() != selected.SizeBytes || offset > info.Size() {
		return nil, offset, false, fmt.Errorf("sealed output object does not match manifest")
	}
	if offset == info.Size() {
		return nil, offset, true, nil
	}
	data := make([]byte, min64(limit, info.Size()-offset))
	n, readErr := file.ReadAt(data, offset)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, offset, false, readErr
	}
	next := offset + int64(n)
	return data[:n], next, next == info.Size(), nil
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
