package imagefsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/sirupsen/logrus"
)

func (mgr *manager) runStatsCommand(cmdName string, out any) error {
	chunkDBDir := filepath.Join(mgr.root, "chunk_db")
	cmd := exec.Command(mgr.binPath, cmdName, "--chunk-db-dir", chunkDBDir)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run %s: %w, stderr: %s", cmdName, err, stripANSI(stderr.String()))
	}

	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("failed to parse %s output: %w, stdout: %s, stderr: %s", cmdName, err, stripANSI(stdout.String()), stripANSI(stderr.String()))
	}

	return nil
}

// checkChunkDBStats runs 'imagefsd stats-chunk' and returns the parsed stats
func (mgr *manager) checkChunkDBStats() (*ChunkDBStats, error) {
	var stats ChunkDBStats
	if err := mgr.runStatsCommand("stats-chunk", &stats); err != nil {
		return nil, err
	}

	logrus.Debugf("stats-chunk: %s", formatChunkDBStats(&stats))

	return &stats, nil
}

func (mgr *manager) ChunkDBStats() (*ChunkDBStats, error) {
	return mgr.checkChunkDBStats()
}

func (mgr *manager) LocalityStats() (*LocalityStats, error) {
	var stats LocalityStats
	if err := mgr.runStatsCommand("stats-locality", &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

// stripANSI removes ANSI escape codes from the given string
func stripANSI(s string) string {
	// Regex pattern matches all ANSI escape sequences
	ansiPattern := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiPattern.ReplaceAllString(s, "")
}

// formatBytes converts bytes to human-readable format (KB, MB, GB, TB)
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.2f %s", float64(bytes)/float64(div), units[exp])
}

// formatChunkDBStats formats ChunkDB stats into a human-readable string
func formatChunkDBStats(stats *ChunkDBStats) string {
	newestTime := time.Unix(stats.AccessTime.NewestEpochSecs, 0).Format("2006-01-02 15:04:05")
	oldestTime := time.Unix(stats.AccessTime.OldestEpochSecs, 0).Format("2006-01-02 15:04:05")

	return fmt.Sprintf("chunks=%d, readers=%d/%d(stale_cleared=%d), usage=%s%%, used=%s, free=%s, total=%s, access_time=[%s ~ %s]",
		stats.Chunks.TotalCount,
		stats.Readers.Current,
		stats.Readers.Max,
		stats.Readers.StaleCleared,
		stats.Storage.UsagePercent,
		formatBytes(stats.Storage.UsedSizeBytes),
		formatBytes(stats.Storage.FreeSizeBytes),
		formatBytes(stats.Storage.TotalSizeBytes),
		oldestTime,
		newestTime)
}

// gcChunkDB runs 'imagefsd gc-chunk' to clean up the ChunkDB
func (mgr *manager) gcChunkDB() error {
	chunkDBDir := filepath.Join(mgr.root, "chunk_db")
	cmd := exec.Command(mgr.binPath, "gc-chunk", "--chunk-db-dir", chunkDBDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run gc-chunk: %w, output: %s", err, stripANSI(string(output)))
	}

	logrus.Infof("gc-chunk completed successfully, output: %s", stripANSI(string(output)))
	return nil
}

// chunkDBCleanupWorker periodically checks ChunkDB usage and triggers cleanup when needed
func (mgr *manager) chunkDBCleanupWorker() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		stats, err := mgr.checkChunkDBStats()
		if err != nil {
			logrus.Errorf("failed to check ChunkDB stats: %v", err)
			continue
		}

		// Log the stats
		logrus.Infof("ChunkDB stats: %s", formatChunkDBStats(stats))

		// Parse usage percentage and check if cleanup is needed
		var usagePercent float64
		if _, err := fmt.Sscanf(stats.Storage.UsagePercent, "%f", &usagePercent); err != nil {
			logrus.Errorf("failed to parse usage percent '%s': %v", stats.Storage.UsagePercent, err)
			continue
		}

		if usagePercent > 80 {
			logrus.Warnf("ChunkDB usage (%.2f%%) exceeds 80%%, triggering cleanup", usagePercent)
			if err := mgr.gcChunkDB(); err != nil {
				logrus.Errorf("failed to cleanup ChunkDB: %v", err)
			} else {
				logrus.Infof("ChunkDB cleanup completed successfully")

				// Check stats again after cleanup
				if newStats, err := mgr.checkChunkDBStats(); err == nil {
					logrus.Infof("ChunkDB stats after cleanup: %s", formatChunkDBStats(newStats))
				}
			}
		}
	}
}
