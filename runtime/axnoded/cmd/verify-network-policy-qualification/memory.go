package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The scenario runner owns this exact disposable domain. Never discover or
// enumerate unrelated host cgroups or process command lines.
func qualificationWorkloadRoot() (string, error) {
	root := filepath.Clean(os.Getenv("AXERN_QUALIFICATION_WORKLOAD_CGROUP"))
	if !strings.HasPrefix(root, "/sys/fs/cgroup/") || filepath.Base(root) != "sandbox" {
		return "", fmt.Errorf("qualification workload cgroup is required")
	}
	return root, nil
}

func waitSampleRetirement(ctx context.Context) error {
	root, err := qualificationWorkloadRoot()
	if err != nil {
		return err
	}
	return waitEmptyCgroup(ctx, root)
}

func waitEmptyCgroup(ctx context.Context, root string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		empty := true
		for _, entry := range entries {
			if entry.IsDir() {
				empty = false
				break
			}
		}
		if empty {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("workload cgroups remain: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func boundedDiagnosticFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "unavailable"
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return "unavailable"
	}
	if len(data) > 4096 {
		return "over_limit"
	}
	return strings.TrimSpace(string(data))
}

func dumpMemoryDiagnostics(sample int, phase string) {
	root, err := qualificationWorkloadRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	paths := []string{}
	// Include ancestor-local events: hierarchical oom_kill alone cannot locate
	// the boundary that killed a process (self-tests intentionally cause OOM).
	for path := filepath.Dir(root); strings.HasPrefix(path, "/sys/fs/cgroup"); path = filepath.Dir(path) {
		paths = append(paths, path)
		if path == "/sys/fs/cgroup" {
			break
		}
	}
	for _, domain := range []string{root, filepath.Join(filepath.Dir(root), "conformance"), filepath.Join(filepath.Dir(root), "internal")} {
		_ = filepath.WalkDir(domain, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return filepath.SkipDir
			}
			if !entry.IsDir() {
				return nil
			}
			if len(paths) >= 64 || strings.Count(strings.TrimPrefix(path, domain), "/") > 2 {
				return filepath.SkipDir
			}
			paths = append(paths, path)
			return nil
		})
	}
	encoder := json.NewEncoder(os.Stderr)
	for i, path := range paths {
		label := fmt.Sprintf("ancestor-%d", i)
		if relative, err := filepath.Rel(filepath.Dir(root), path); err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, "../") {
			label = relative
		}
		values := map[string]string{}
		for _, name := range []string{"memory.current", "memory.peak", "memory.max", "memory.swap.max", "memory.oom.group", "memory.events", "memory.events.local", "memory.stat", "cgroup.procs"} {
			values[name] = boundedDiagnosticFile(filepath.Join(path, name))
		}
		// Paths are restricted to the owned test hierarchy; no DNS/policy or
		// command/environment payload is included.
		_ = encoder.Encode(struct {
			Sample int               `json:"sample"`
			Phase  string            `json:"phase"`
			Index  int               `json:"index"`
			Cgroup string            `json:"cgroup"`
			Values map[string]string `json:"values"`
		}{sample, phase, i, label, values})
	}
}
