package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/egressd/internal/qualification"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "egressd-qualify: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("command is required: assemble, subject, validate, or compare")
	}
	switch args[0] {
	case "assemble":
		return runAssemble(args[1:], stdout)
	case "subject":
		return runSubject(args[1:], stdout)
	case "validate":
		return runValidate(args[1:], stdout)
	case "compare":
		return runCompare(args[1:], stdout)
	default:
		return fmt.Errorf("unsupported command %q", args[0])
	}
}

func runSubject(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("subject", flag.ContinueOnError)
	path := flags.String("file", "", "embedded qualification subject commit file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *path == "" {
		return errors.New("subject requires exactly -file")
	}
	commit, err := qualification.ReadSubjectCommit(*path)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, commit)
	return err
}

func runValidate(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	reportPath := flags.String("report", "", "qualification report JSON")
	fullMatrix := flags.Bool("full-matrix", true, "require the complete qualification matrix")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *reportPath == "" {
		return errors.New("validate requires exactly -report")
	}
	report, err := qualification.ReadReport(*reportPath)
	if err != nil {
		return err
	}
	if err := report.Validate(*fullMatrix); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "network_policy_qualification_valid=true")
	return err
}

func runCompare(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	baselinePath := flags.String("baseline", "", "baseline report JSON")
	candidatePath := flags.String("candidate", "", "candidate report JSON")
	budgetPath := flags.String("budget", "", "relative regression budget JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *baselinePath == "" || *candidatePath == "" || *budgetPath == "" {
		return errors.New("compare requires -baseline, -candidate, and -budget")
	}
	baseline, err := qualification.ReadReport(*baselinePath)
	if err != nil {
		return err
	}
	candidate, err := qualification.ReadReport(*candidatePath)
	if err != nil {
		return err
	}
	budgetFile, err := os.Open(*budgetPath)
	if err != nil {
		return err
	}
	budget, decodeErr := qualification.DecodeBudget(budgetFile)
	closeErr := budgetFile.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	comparison, err := qualification.Compare(baseline, candidate, budget)
	if err != nil {
		return err
	}
	if err := qualification.WriteComparison(stdout, comparison); err != nil {
		return err
	}
	if !comparison.Passed {
		return fmt.Errorf("candidate exceeds %d network-policy qualification budget(s)", len(comparison.Violations))
	}
	return nil
}

func runAssemble(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("assemble", flag.ContinueOnError)
	samplesDir := flags.String("scenarios", "", "directory containing one scenario JSON per matrix cell")
	commit := flags.String("subject-commit", "", "candidate git commit")
	buildDigest := flags.String("subject-build-digest", "", "candidate build-set sha256 digest")
	dirty := flags.Bool("subject-dirty", false, "mark the candidate checkout dirty")
	hostIdentityDigest := flags.String("host-identity-digest", "", "sha256 digest of the stable Linux host identity")
	runcBinary := flags.String("runc-binary", "", "runc binary used by the matrix")
	runscBinary := flags.String("runsc-binary", "", "runsc binary used by the matrix")
	samples := flags.Int("samples", 0, "samples per latency distribution")
	concurrency := flags.Int("concurrency", 0, "target concurrent sessions")
	payloadBytes := flags.Int("payload-bytes", 0, "relay payload size")
	sustainedSeconds := flags.Int("sustained-seconds", 0, "sustained reliability interval")
	ruleScaleRaw := flags.String("rule-scale-counts", "", "comma-separated rule scale counts")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *samplesDir == "" || *commit == "" || *buildDigest == "" || *hostIdentityDigest == "" || *runcBinary == "" || *runscBinary == "" {
		return errors.New("assemble requires scenarios, subject identity, host identity digest, and both runtime binaries")
	}
	ruleScaleCounts, err := parseRuleScaleCounts(*ruleScaleRaw)
	if err != nil {
		return err
	}
	environment, err := captureEnvironment(*hostIdentityDigest, map[string]string{"runc": *runcBinary, "runsc": *runscBinary})
	if err != nil {
		return err
	}
	scenarios, err := readScenarios(*samplesDir)
	if err != nil {
		return err
	}
	report := qualification.Report{
		SchemaVersion: qualification.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Environment:   environment,
		Subject:       qualification.SubjectProvenance{Commit: *commit, Dirty: *dirty, Build: *buildDigest},
		Parameters: qualification.Parameters{
			Samples: *samples, Concurrency: *concurrency, PayloadBytes: *payloadBytes,
			SustainedSeconds: *sustainedSeconds, RuleScaleCounts: ruleScaleCounts,
		},
		Scenarios: scenarios,
	}
	report.Normalize()
	if err := report.Validate(true); err != nil {
		return err
	}
	return qualification.WriteReport(stdout, report)
}

func readScenarios(root string) ([]qualification.ScenarioResult, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			paths = append(paths, filepath.Join(root, entry.Name()))
		}
	}
	sort.Strings(paths)
	scenarios := make([]qualification.ScenarioResult, 0, len(paths))
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		scenario, decodeErr := qualification.DecodeScenario(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("%s: %w", path, decodeErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		scenarios = append(scenarios, scenario)
	}
	return scenarios, nil
}

func captureEnvironment(hostIdentityDigest string, runtimeBinaries map[string]string) (qualification.EnvironmentProvenance, error) {
	if runtime.GOOS != "linux" {
		return qualification.EnvironmentProvenance{}, errors.New("qualification provenance capture requires Linux")
	}
	kernel, err := readTrimmed("/proc/sys/kernel/osrelease")
	if err != nil {
		return qualification.EnvironmentProvenance{}, err
	}
	cpuModel, err := readCPUModel("/proc/cpuinfo")
	if err != nil {
		return qualification.EnvironmentProvenance{}, err
	}
	memoryBytes, err := readMemoryBytes("/proc/meminfo")
	if err != nil {
		return qualification.EnvironmentProvenance{}, err
	}
	packagesDigest, err := systemPackagesDigest()
	if err != nil {
		return qualification.EnvironmentProvenance{}, err
	}
	digests := map[string]string{}
	for name, path := range runtimeBinaries {
		digest, err := fileDigest(path)
		if err != nil {
			return qualification.EnvironmentProvenance{}, fmt.Errorf("hash %s runtime: %w", name, err)
		}
		digests[name] = digest
	}
	environment := qualification.EnvironmentProvenance{
		OS: runtime.GOOS, Architecture: runtime.GOARCH, KernelRelease: kernel, CPUModel: cpuModel,
		LogicalCPUs: runtime.NumCPU(), MemoryBytes: memoryBytes, HostIdentityDigest: hostIdentityDigest,
		SystemPackagesDigest: packagesDigest,
		RuntimeDigests:       digests,
	}
	environment.EnvironmentID, err = environment.Fingerprint()
	return environment, err
}

func systemPackagesDigest() (string, error) {
	command := exec.Command("dpkg-query", "-W", "-f=${binary:Package}=${Version}:${db:Status-Abbrev}\\n")
	wire, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("capture system package manifest: %w", err)
	}
	return packageManifestDigest(wire)
}

func packageManifestDigest(wire []byte) (string, error) {
	lines := strings.Split(strings.TrimSpace(string(wire)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return "", errors.New("system package manifest is empty")
	}
	sort.Strings(lines)
	digest := sha256.Sum256([]byte(strings.Join(lines, "\n") + "\n"))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func readTrimmed(path string) (string, error) {
	wire, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(wire))
	if value == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return value, nil
}

func readCPUModel(path string) (string, error) {
	wire, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(wire), "\n") {
		key, value, found := strings.Cut(line, ":")
		if found && (strings.TrimSpace(key) == "model name" || strings.TrimSpace(key) == "Hardware") {
			if value = strings.TrimSpace(value); value != "" {
				return value, nil
			}
		}
	}
	return "", errors.New("CPU model is absent from /proc/cpuinfo")
}

func readMemoryBytes(path string) (uint64, error) {
	wire, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(wire), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "MemTotal:" && fields[2] == "kB" {
			kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return kilobytes * 1024, nil
		}
	}
	return 0, errors.New("MemTotal is absent from /proc/meminfo")
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func parseRuleScaleCounts(raw string) ([]uint32, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("rule-scale-counts is required")
	}
	seen := map[uint32]struct{}{}
	values := []uint32{}
	for _, part := range strings.Split(raw, ",") {
		value, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32)
		if err != nil || value == 0 {
			return nil, fmt.Errorf("invalid rule-scale count %q", part)
		}
		count := uint32(value)
		if _, exists := seen[count]; exists {
			continue
		}
		seen[count] = struct{}{}
		values = append(values, count)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values, nil
}
