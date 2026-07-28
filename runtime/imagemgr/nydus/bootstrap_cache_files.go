package nydus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

func (r *bootstrapCacheRoot) cachePath(key string) string {
	return filepath.Join(r.root, key+bootstrapCacheFileExt)
}

func (r *bootstrapCacheRoot) envPath(key string) string {
	return filepath.Join(r.root, key+bootstrapCacheEnvExt)
}

func bootstrapCacheKey(imageURL string) string {
	sum := sha256.Sum256([]byte(imageURL))
	return hex.EncodeToString(sum[:])
}

func bootstrapOutputPath(outputDir string) string {
	return filepath.Join(outputDir, bootstrapCacheOutputName)
}

func bootstrapCacheLogFields(imageURL string, key string, cacheRoot string, cachePath string, outputPath string, bootstrapPath string) logrus.Fields {
	fields := logrus.Fields{}
	if imageURL != "" {
		fields["image_url"] = imageURL
	}
	if key != "" {
		fields["cache_key"] = key
	}
	if cacheRoot != "" {
		fields["cache_root"] = cacheRoot
	}
	if cachePath != "" {
		fields["cache_path"] = cachePath
	}
	if outputPath != "" {
		fields["output_path"] = outputPath
	}
	if bootstrapPath != "" {
		fields["bootstrap_path"] = bootstrapPath
	}
	return fields
}

func ensureHardLink(sourcePath string, targetPath string) error {
	if sameFile(sourcePath, targetPath) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create bootstrap parent dir for %s: %w", targetPath, err)
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to replace bootstrap target %s: %w", targetPath, err)
	}
	if err := os.Link(sourcePath, targetPath); err != nil {
		return err
	}
	return nil
}

func touchFile(path string, now time.Time) error {
	return os.Chtimes(path, now, now)
}

func sameFile(pathA string, pathB string) bool {
	if pathA == "" || pathB == "" {
		return false
	}
	infoA, err := os.Stat(pathA)
	if err != nil {
		return false
	}
	infoB, err := os.Stat(pathB)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// writeEnvSidecar atomically writes env vars as a JSON array to the sidecar path.
// Uses write-to-temp-then-rename to avoid leaving a corrupted file on crash.
// A nil env is written as an empty JSON array ([]). The presence of the
// file itself signals that env was cached.
func writeEnvSidecar(path string, env []string) error {
	if env == nil {
		env = []string{}
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readEnvSidecar reads a cached env sidecar file.
// Returns nil if the file does not exist (old cache entry without env).
// Returns a non-nil slice (possibly empty) if the file was read.
func readEnvSidecar(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var env []string
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}
	return env
}
