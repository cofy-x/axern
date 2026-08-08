//go:build linux

package rootfsview

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func applyProjectQuota(filestoreDir, upperDir string, projectID uint32, limitBytes int64) error {
	if projectID == 0 || limitBytes <= 0 {
		return fmt.Errorf("runc writable rootfs requires a positive XFS project ID and limit")
	}
	if !safeXFSQuotaPath(filestoreDir) || !safeXFSQuotaPath(upperDir) {
		return fmt.Errorf("XFS project quota paths contain unsupported characters")
	}
	project := strconv.FormatUint(uint64(projectID), 10)
	commands := []string{
		"project -s -p " + upperDir + " " + project,
		"limit -p bhard=" + strconv.FormatInt(limitBytes, 10) + " bsoft=" + strconv.FormatInt(limitBytes, 10) + " " + project,
	}
	for _, command := range commands {
		output, err := exec.Command("xfs_quota", "-x", "-c", command, filestoreDir).CombinedOutput()
		if err != nil {
			return fmt.Errorf("apply XFS project quota (%s): %s: %w", command, strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

func safeXFSQuotaPath(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '/' || character == '.' || character == '_' || character == '-' || character == '+' {
			continue
		}
		return false
	}
	return true
}
