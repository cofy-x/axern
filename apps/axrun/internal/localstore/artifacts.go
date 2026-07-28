package localstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func (s Store) WriteAgentArtifact(artifactDir string, filename string, content string) (string, error) {
	if err := contract.ValidatePathSegment("artifact filename", filename); err != nil {
		return "", err
	}
	if strings.TrimSpace(artifactDir) == "" {
		return "", fmt.Errorf("artifact directory is required")
	}
	path := filepath.Join(artifactDir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write artifact %s: %w", filename, err)
	}
	return runRelativePath(runDirFromArtifactDir(artifactDir), path), nil
}

func (s Store) AppendAgentRawEvent(artifactDir string, event domain.AgentRawEvent) (string, error) {
	if strings.TrimSpace(artifactDir) == "" {
		return "", fmt.Errorf("artifact directory is required")
	}
	if !contract.IsAgentRawEventType(event.Type) {
		return "", fmt.Errorf("unsupported agent raw event type %q", event.Type)
	}
	path := filepath.Join(artifactDir, "agent.raw.jsonl")
	if event.EventID == "" {
		eventID, err := nextAgentRawEventID(path)
		if err != nil {
			return "", err
		}
		event.EventID = eventID
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("marshal agent raw event: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("open agent raw log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(payload, '\n')); err != nil {
		return "", fmt.Errorf("append agent raw event: %w", err)
	}
	return runRelativePath(runDirFromArtifactDir(artifactDir), path), nil
}

func nextAgentRawEventID(path string) (string, error) {
	count, err := countNonEmptyLines(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("raw-%06d", count+1), nil
}

func countNonEmptyLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("count agent raw events: %w", err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan agent raw log: %w", err)
	}
	return count, nil
}
