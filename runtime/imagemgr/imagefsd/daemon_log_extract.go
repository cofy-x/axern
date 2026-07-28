package imagefsd

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

const daemonLogStagingDir = "daemon_log_staging"

// logAggEntry holds an aggregated log entry for deduplication.
type logAggEntry struct {
	level   logrus.Level
	message string
	count   int
}

// Regex to strip the tracing-subscriber Full format timestamp prefix.
// Example: "2026-04-08T10:23:45.123456Z  " -> ""
var timestampPrefixRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s+`)

// trailingKVRe strips trailing key=value structured fields from a log message
// so that lines differing only in field values (e.g. count=1 vs count=8) are
// aggregated together.
var trailingKVRe = regexp.MustCompile(`(\s+\w+=\S+)+$`)

// rescueDaemonLog moves the daemon log file to a staging directory and
// launches a goroutine to extract WARN/ERROR lines. If the rename fails,
// extraction is skipped entirely to avoid blocking the cleanup path.
func rescueDaemonLog(root string, d *Daemon) {
	if d.meta.DaemonLogPath == "" {
		return
	}

	stagingDir := filepath.Join(root, daemonLogStagingDir)
	stagedPath := filepath.Join(stagingDir, d.meta.ID+".log")

	if err := os.Rename(d.meta.DaemonLogPath, stagedPath); err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Debug("skipping daemon log extraction: rename failed")
		return
	}

	// Capture fields now while Daemon is still accessible
	fields := d.daemonLogFields()

	go processDaemonLog(stagedPath, fields)
}

// processDaemonLog reads a staged daemon log file, extracts WARN and ERROR
// lines, aggregates duplicates, and outputs them via logrus. The staged file
// is always removed after processing.
func processDaemonLog(logFilePath string, daemonFields logrus.Fields) {
	defer os.Remove(logFilePath)

	file, err := os.Open(logFilePath)
	if err != nil {
		logrus.WithError(err).Warn("failed to open staged daemon log for extraction")
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	aggregated := make(map[string]*logAggEntry)
	var totalWarn, totalError int

	for scanner.Scan() {
		line := scanner.Text()
		level, displayMsg, dedupKey := classifyLogLine(line)
		if level == 0 {
			continue
		}

		if level == logrus.WarnLevel {
			totalWarn++
		} else {
			totalError++
		}

		if entry, ok := aggregated[dedupKey]; ok {
			entry.count++
		} else {
			aggregated[dedupKey] = &logAggEntry{
				level:   level,
				message: displayMsg,
				count:   1,
			}
		}
	}

	if err := scanner.Err(); err != nil {
		logrus.WithFields(daemonFields).WithError(err).Warn("error reading staged daemon log")
	}

	if len(aggregated) == 0 {
		return
	}

	// Output aggregated logs
	for _, entry := range aggregated {
		fields := make(logrus.Fields, len(daemonFields)+2)
		for k, v := range daemonFields {
			fields[k] = v
		}
		fields["source"] = "imagefsd"
		if entry.count > 1 {
			fields["occurrences"] = entry.count
		}

		logEntry := logrus.WithFields(fields)
		if entry.level == logrus.WarnLevel {
			logEntry.Warn(entry.message)
		} else {
			logEntry.Error(entry.message)
		}
	}

	// Summary line
	uniqueWarn := countByLevel(aggregated, logrus.WarnLevel)
	uniqueError := countByLevel(aggregated, logrus.ErrorLevel)
	logrus.WithFields(daemonFields).WithField("source", "imagefsd").
		Infof("daemon log extraction: %d unique WARN, %d unique ERROR (from %d total WARN, %d total ERROR)",
			uniqueWarn, uniqueError, totalWarn, totalError)
}

// classifyLogLine checks if a log line is WARN or ERROR level and returns
// the level, a display message (timestamp stripped), and a dedup key (also
// with trailing key=value fields stripped so that messages differing only in
// structured field values are aggregated together).
// Returns (0, "", "") if the line is not WARN or ERROR.
//
// Expected format (tracing-subscriber Full):
//
//	2026-04-08T10:23:45.123456Z  WARN imagefsd::module: message text
//	2026-04-08T10:23:45.123456Z ERROR imagefsd::module: message text
func classifyLogLine(line string) (level logrus.Level, displayMsg string, dedupKey string) {
	// Strip timestamp prefix to get " WARN ..." or "ERROR ..."
	stripped := timestampPrefixRe.ReplaceAllString(line, "")
	if stripped == line && len(line) > 0 {
		// No timestamp prefix found — not the expected format, but still
		// try to match level at the start of the line
	}

	// Detect level: " WARN" (space-padded) or "ERROR" at start of stripped line
	if strings.HasPrefix(stripped, "WARN ") {
		level = logrus.WarnLevel
	} else if strings.HasPrefix(stripped, "ERROR ") {
		level = logrus.ErrorLevel
	} else {
		return 0, "", ""
	}

	displayMsg = strings.TrimSpace(stripped)
	dedupKey = trailingKVRe.ReplaceAllString(displayMsg, "")
	return level, displayMsg, dedupKey
}

func countByLevel(aggregated map[string]*logAggEntry, level logrus.Level) int {
	count := 0
	for _, entry := range aggregated {
		if entry.level == level {
			count++
		}
	}
	return count
}
