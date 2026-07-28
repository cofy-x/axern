package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func NewRolloutRunID(now time.Time) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate run id suffix: %w", err)
	}
	return fmt.Sprintf("run_%s_%s", now.UTC().Format("20060102T150405Z"), hex.EncodeToString(suffix[:])), nil
}

func NewEpisodeID(runID string, taskID string, attemptIndex int) string {
	return fmt.Sprintf("episode_%s_%s_%d", runID, taskID, attemptIndex)
}
