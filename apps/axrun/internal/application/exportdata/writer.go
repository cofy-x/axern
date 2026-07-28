package exportdata

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func ensureOutputPath(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("export output already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat export output: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create export output directory: %w", err)
	}
	return nil
}

func writeExportFile(outputPath string, records []any) error {
	if err := ensureOutputPath(outputPath); err != nil {
		return err
	}
	outputDir := filepath.Dir(outputPath)
	tmp, err := os.CreateTemp(outputDir, "."+filepath.Base(outputPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary export output: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	writer := bufio.NewWriter(tmp)
	for _, record := range records {
		if err := writeJSONLine(writer, record); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flush export output: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close export output: %w", err)
	}
	if err := os.Link(tmpPath, outputPath); err != nil {
		return fmt.Errorf("publish export output: %w", err)
	}
	committed = true
	return os.Remove(tmpPath)
}

func writeJSONLine(writer *bufio.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal export record: %w", err)
	}
	if _, err := writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write export record: %w", err)
	}
	return nil
}
