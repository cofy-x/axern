package schema

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func validateAgentRawLog(problems *collector, runDir string, ref string) {
	path, ok := runRefPath(runDir, ref)
	if !ok {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		problems.add(displayPath(runDir, path), "", fmt.Sprintf("open agent raw log: %v", err))
		return
	}
	defer file.Close()
	rel := displayPath(runDir, path)
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var event domain.AgentRawEvent
		if err := json.Unmarshal([]byte(text), &event); err != nil {
			problems.add(rel, fmt.Sprintf("line %d", line), fmt.Sprintf("decode agent raw event: %v", err))
			continue
		}
		validateAgentRawEvent(problems, runDir, rel, line, event)
	}
	if err := scanner.Err(); err != nil {
		problems.add(rel, "", fmt.Sprintf("scan agent raw log: %v", err))
	}
}

func validateAgentRawEvent(problems *collector, runDir string, rel string, line int, event domain.AgentRawEvent) {
	prefix := fmt.Sprintf("line %d", line)
	if !contract.IsAgentRawEventType(event.Type) {
		problems.add(rel, prefix+".type", fmt.Sprintf("unsupported agent raw event type %q", event.Type))
	}
	if !contract.IsAgentLauncherKind(event.LauncherKind) {
		problems.add(rel, prefix+".launcher_kind", fmt.Sprintf("unsupported agent launcher kind %q", event.LauncherKind))
	}
	if event.RuntimeType != "" && !contract.IsAgentRuntimeType(event.RuntimeType) {
		problems.add(rel, prefix+".runtime_type", fmt.Sprintf("unsupported agent runtime type %q", event.RuntimeType))
	}
	validateAgentRawEventRef(problems, runDir, rel, prefix+".body_ref", event.BodyRef)
	validateAgentRawEventRef(problems, runDir, rel, prefix+".chunk_ref", event.ChunkRef)
	validateAgentRawEventRef(problems, runDir, rel, prefix+".command_ref", event.CommandRef)
	validateAgentRawEventRef(problems, runDir, rel, prefix+".stdout_ref", event.StdoutRef)
	validateAgentRawEventRef(problems, runDir, rel, prefix+".stderr_ref", event.StderrRef)
	validateAgentRawEventRef(problems, runDir, rel, prefix+".artifact_ref", event.ArtifactRef)
	validateAgentRawEventRef(problems, runDir, rel, prefix+".patch_ref", event.PatchRef)
	if event.ArtifactKind != "" && !contract.IsArtifactKind(event.ArtifactKind) {
		problems.add(rel, prefix+".artifact_kind", fmt.Sprintf("unsupported artifact kind %q", event.ArtifactKind))
	}
}

func validateAgentRawEventRef(problems *collector, runDir string, path string, field string, ref string) {
	if strings.TrimSpace(ref) == "" {
		return
	}
	validateRunRef(problems, runDir, path, field, ref, false)
	validateExistingRunRef(problems, runDir, path, field, ref)
}
