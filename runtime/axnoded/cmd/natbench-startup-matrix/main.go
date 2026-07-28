package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

func main() {
	var (
		mode       = flag.String("mode", "scenario", "scenario or matrix")
		sampleDir  = flag.String("sample-dir", "", "directory containing startup sample reports for one scenario")
		reportsDir = flag.String("reports-dir", "", "directory containing per-scenario startup reports")
	)
	flag.Parse()

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "scenario":
		if strings.TrimSpace(*sampleDir) == "" {
			fatalf("-sample-dir is required for mode=scenario")
		}
		samples, err := loadScenarioSamples(*sampleDir)
		if err != nil {
			fatalf("load scenario samples: %v", err)
		}
		report, err := natbench.BuildStartupScenarioReport(samples)
		if err != nil {
			fatalf("build scenario report: %v", err)
		}
		encode(report)
	case "matrix":
		if strings.TrimSpace(*reportsDir) == "" {
			fatalf("-reports-dir is required for mode=matrix")
		}
		reports, err := loadScenarioReports(*reportsDir)
		if err != nil {
			fatalf("load scenario reports: %v", err)
		}
		report := natbench.BuildStartupMatrixReport(reports)
		encode(report)
	default:
		fatalf("unsupported mode %q", *mode)
	}
}

func loadScenarioSamples(dir string) ([]natbench.StartupScenarioSampleReport, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	reports := make([]natbench.StartupScenarioSampleReport, 0, len(paths))
	for _, path := range paths {
		report, err := natbench.ReadStartupScenarioSampleReport(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func loadScenarioReports(dir string) ([]natbench.StartupScenarioReport, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	reports := make([]natbench.StartupScenarioReport, 0, len(paths))
	for _, path := range paths {
		report, err := natbench.ReadStartupScenarioReport(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func encode(payload any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		fatalf("encode report: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
