package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

func main() {
	var (
		iptablesDir = flag.String("iptables-dir", "", "directory containing iptables benchmark JSON reports")
		ebpfDir     = flag.String("ebpf-dir", "", "directory containing ebpf benchmark JSON reports")
		expectRuns  = flag.Int("expect-runs", 0, "optional expected report count per backend")
	)
	flag.Parse()

	if *iptablesDir == "" || *ebpfDir == "" {
		fatalf("iptables-dir and ebpf-dir are required")
	}

	iptablesReports, err := readReports(*iptablesDir)
	if err != nil {
		fatalf("read iptables reports: %v", err)
	}
	ebpfReports, err := readReports(*ebpfDir)
	if err != nil {
		fatalf("read ebpf reports: %v", err)
	}
	if *expectRuns > 0 {
		if len(iptablesReports) != *expectRuns {
			fatalf("expected %d iptables reports, got %d", *expectRuns, len(iptablesReports))
		}
		if len(ebpfReports) != *expectRuns {
			fatalf("expected %d ebpf reports, got %d", *expectRuns, len(ebpfReports))
		}
	}

	compare, err := natbench.BuildCompareReport(iptablesReports, ebpfReports)
	if err != nil {
		fatalf("build compare report: %v", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(compare); err != nil {
		fatalf("encode compare report: %v", err)
	}
}

func readReports(dir string) ([]natbench.Report, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no json reports found in %s", dir)
	}
	sort.Strings(entries)

	reports := make([]natbench.Report, 0, len(entries))
	for _, entry := range entries {
		report, err := natbench.ReadReport(entry)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry, err)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
