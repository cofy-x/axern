package exportdata

import (
	"fmt"
	"path/filepath"
	"strings"

	validateapp "github.com/cofy-x/axern/apps/axrun/internal/application/validate"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

const (
	sftSchemaVersion    = "axrun.export.sft"
	rewardSchemaVersion = "axrun.export.reward"
	traceSchemaVersion  = "axrun.export.trace"
)

func Export(params Params) (Result, error) {
	if err := validate(params); err != nil {
		return Result{}, err
	}
	runDir := filepath.Clean(params.RunDir)
	outputPath := params.OutputPath
	if strings.TrimSpace(outputPath) == "" {
		outputPath = filepath.Join(runDir, "exports", string(params.Format)+".jsonl")
	}
	if _, err := validateapp.Run(validateapp.Params{RunDir: runDir}); err != nil {
		return Result{}, err
	}
	run, err := readJSONFile[domain.RolloutRun](filepath.Join(runDir, "run.json"))
	if err != nil {
		return Result{}, err
	}
	episodes, err := loadEpisodes(runDir)
	if err != nil {
		return Result{}, err
	}

	var records []any
	if params.Format == FormatPreference {
		records, err = exportPreference(runDir, outputPath, run, episodes)
		if err != nil {
			return Result{}, err
		}
	} else {
		records = make([]any, 0, len(episodes))
		for _, episode := range episodes {
			bundle, err := loadEpisodeBundle(runDir, outputPath, run, episode)
			if err != nil {
				return Result{}, err
			}
			built, err := buildRecords(bundle, params.Format)
			if err != nil {
				return Result{}, err
			}
			records = append(records, built...)
		}
	}

	if err := writeExportFile(outputPath, records); err != nil {
		return Result{}, err
	}
	return Result{Format: params.Format, RunID: run.ID, OutputPath: outputPath, RecordCount: len(records)}, nil
}

func validate(params Params) error {
	if strings.TrimSpace(params.RunDir) == "" {
		return fmt.Errorf("run directory is required")
	}
	switch params.Format {
	case FormatSFT, FormatReward, FormatTrace, FormatPreference:
		return nil
	default:
		return fmt.Errorf("unsupported export format %q", params.Format)
	}
}
