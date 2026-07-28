package exportdata

import (
	"fmt"
	"path/filepath"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/redact"
)

func buildTraceRecords(bundle episodeBundle) ([]any, error) {
	var records []any
	trajectoryRecords, err := buildTrajectoryTraceRecords(bundle)
	if err != nil {
		return nil, err
	}
	records = append(records, trajectoryRecords...)
	rawRecords, err := buildRawTraceRecords(bundle)
	if err != nil {
		return nil, err
	}
	records = append(records, rawRecords...)
	return records, nil
}

func buildTrajectoryTraceRecords(bundle episodeBundle) ([]any, error) {
	if bundle.Refs.TrajectoryPath == "" {
		return nil, nil
	}
	path := filepath.Join(bundle.RunRoot, filepath.FromSlash(bundle.Refs.TrajectoryPath))
	steps, err := readJSONLines[domain.TrajectoryStep](path)
	if err != nil {
		return nil, err
	}
	records := make([]any, 0, len(steps))
	for _, step := range steps {
		timestamp := step.Timestamp
		if step.Index <= 0 {
			return nil, fmt.Errorf("trajectory step index must be greater than zero")
		}
		record := TraceRecord{
			SchemaVersion:       traceSchemaVersion,
			RecordID:            exportTraceRecordID(bundle.Episode.ID, "trajectory", step.Index),
			SourceSchemaVersion: bundle.Run.SchemaVersion,
			RunID:               bundle.Run.ID,
			EpisodeID:           bundle.Episode.ID,
			TaskID:              bundle.Task.ID,
			AttemptIndex:        bundle.Episode.AttemptIndex,
			Agent:               agentSummary(bundle.Episode.Agent),
			Model:               bundle.Episode.Model,
			Source:              "trajectory",
			Type:                string(step.Type),
			EventID:             step.EventID,
			ParentEventID:       step.ParentEventID,
			Index:               step.Index,
			Timestamp:           &timestamp,
			Actor:               step.Actor,
			Summary:             step.Summary,
			SourceRef:           step.SourceRef,
			InputRef:            step.InputRef,
			OutputRef:           step.OutputRef,
			PayloadRef:          step.PayloadRef,
			RawRef:              step.RawRef,
			LatencyMS:           step.DurationMS,
			Usage:               step.Usage,
			Cost:                step.Cost,
			Artifacts:           step.Artifacts,
			Metadata:            safeTrajectoryMetadata(step.Metadata),
			Refs:                bundle.Refs,
		}
		records = append(records, record)
	}
	return records, nil
}

func buildRawTraceRecords(bundle episodeBundle) ([]any, error) {
	if bundle.Refs.RawLogRef == "" {
		return nil, nil
	}
	path := filepath.Join(bundle.RunRoot, filepath.FromSlash(bundle.Refs.RawLogRef))
	events, err := readJSONLines[domain.AgentRawEvent](path)
	if err != nil {
		return nil, err
	}
	records := make([]any, 0, len(events))
	for index, event := range events {
		record := TraceRecord{
			SchemaVersion:       traceSchemaVersion,
			RecordID:            exportTraceRecordID(bundle.Episode.ID, "agent_raw", index+1),
			SourceSchemaVersion: bundle.Run.SchemaVersion,
			RunID:               bundle.Run.ID,
			EpisodeID:           bundle.Episode.ID,
			TaskID:              bundle.Task.ID,
			AttemptIndex:        bundle.Episode.AttemptIndex,
			Agent:               agentSummary(bundle.Episode.Agent),
			Model:               bundle.Episode.Model,
			Source:              "agent.raw",
			Type:                string(event.Type),
			EventID:             event.EventID,
			Line:                index + 1,
			Timestamp:           event.Timestamp,
			Method:              event.Method,
			Path:                redact.String(event.Path),
			Status:              event.Status,
			LatencyMS:           event.LatencyMS,
			ModelID:             event.Model,
			BodyRef:             event.BodyRef,
			ChunkRef:            event.ChunkRef,
			RequestRef:          event.RequestRef,
			ResponseRef:         event.ResponseRef,
			CommandRef:          event.CommandRef,
			CWD:                 event.CWD,
			User:                event.User,
			TimeoutSec:          event.TimeoutSec,
			LauncherKind:        event.LauncherKind,
			RuntimeType:         event.RuntimeType,
			RuntimeImage:        event.RuntimeImage,
			RuntimeMountTarget:  event.RuntimeMountTarget,
			RuntimeBinDir:       event.RuntimeBinDir,
			RuntimeProfile:      event.RuntimeProfile,
			ExitCode:            event.ExitCode,
			StdoutRef:           event.StdoutRef,
			StderrRef:           event.StderrRef,
			ArtifactRef:         event.ArtifactRef,
			ArtifactKind:        event.ArtifactKind,
			PatchRef:            event.PatchRef,
			ToolName:            event.ToolName,
			ToolCallID:          event.ToolCallID,
			Error:               redact.String(event.Error),
			DroppedEvents:       event.DroppedEvents,
			DroppedBodies:       event.DroppedBodies,
			DroppedBytes:        event.DroppedBytes,
			Usage:               event.Usage,
			Cost:                event.Cost,
			Refs:                bundle.Refs,
		}
		records = append(records, record)
	}
	return records, nil
}

func exportTraceRecordID(episodeID string, source string, sequence int) string {
	return fmt.Sprintf("%s_%s_%s_%06d", FormatTrace, episodeID, source, sequence)
}

func safeTrajectoryMetadata(metadata domain.KeyValue) domain.KeyValue {
	if len(metadata) == 0 {
		return nil
	}
	allowed := map[string]struct{}{
		"clean":                {},
		"consecutive_fails":    {},
		"dirty_count":          {},
		"fatal":                {},
		"fatal_reason":         {},
		"launcher_kind":        {},
		"phase":                {},
		"revision":             {},
		"runtime_bin_dir":      {},
		"runtime_image":        {},
		"runtime_mount_target": {},
		"runtime_profile":      {},
		"runtime_type":         {},
		"source":               {},
		"untracked_count":      {},
	}
	safe := domain.KeyValue{}
	for key, value := range metadata {
		if _, ok := allowed[key]; !ok {
			continue
		}
		safe[key] = redact.String(value)
	}
	if len(safe) == 0 {
		return nil
	}
	return safe
}
