package schema

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func validateTrajectory(problems *collector, runDir string, path string) {
	file, err := os.Open(path)
	if err != nil {
		problems.add(displayPath(runDir, path), "", fmt.Sprintf("open trajectory: %v", err))
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var step domain.TrajectoryStep
		if err := json.Unmarshal([]byte(text), &step); err != nil {
			problems.add(displayPath(runDir, path), fmt.Sprintf("line %d", line), fmt.Sprintf("decode trajectory step: %v", err))
			continue
		}
		validateTrajectoryStep(problems, runDir, displayPath(runDir, path), line, step)
	}
	if err := scanner.Err(); err != nil {
		problems.add(displayPath(runDir, path), "", fmt.Sprintf("scan trajectory: %v", err))
	}
}

func validateTrajectoryStep(problems *collector, runDir string, rel string, line int, step domain.TrajectoryStep) {
	prefix := fmt.Sprintf("line %d", line)
	if step.Index < 1 {
		problems.add(rel, prefix+".index", "must be greater than or equal to one")
	}
	validateTrajectoryEventType(problems, rel, prefix+".type", step.Type)
	problems.required(rel, prefix+".actor", step.Actor)
	problems.required(rel, prefix+".summary", step.Summary)
	validateRunRef(problems, runDir, rel, prefix+".source_ref", step.SourceRef, false)
	validateRunRef(problems, runDir, rel, prefix+".input_ref", step.InputRef, false)
	validateRunRef(problems, runDir, rel, prefix+".output_ref", step.OutputRef, false)
	validateRunRef(problems, runDir, rel, prefix+".payload_ref", step.PayloadRef, false)
	validateRunRef(problems, runDir, rel, prefix+".raw_ref", step.RawRef, false)
	validateArtifactRefs(problems, runDir, rel, prefix+".artifacts", step.Artifacts)
}
