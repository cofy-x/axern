package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseRuleScaleCountsNormalizesAndDeduplicates(t *testing.T) {
	got, err := parseRuleScaleCounts("256,1,64,1")
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{1, 64, 256}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRuleScaleCounts() = %v, want %v", got, want)
	}
	if _, err := parseRuleScaleCounts("0"); err == nil {
		t.Fatal("zero rule count was accepted")
	}
}

func TestPackageManifestDigestIsOrderIndependent(t *testing.T) {
	first, err := packageManifestDigest([]byte("nftables=1.0.9\niproute2=6.1.0\n"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := packageManifestDigest([]byte("iproute2=6.1.0\nnftables=1.0.9\n"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("manifest digests = %q and %q", first, second)
	}
	if _, err := packageManifestDigest(nil); err == nil {
		t.Fatal("empty package manifest was accepted")
	}
}

func TestRunSubjectReadsEmbeddedCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subject")
	commit := strings.Repeat("a", 40)
	if err := os.WriteFile(path, []byte(commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := run([]string{"subject", "-file", path}, &output); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(output.String()); got != commit {
		t.Fatalf("subject output = %q, want %q", got, commit)
	}
}

func TestReadLinuxProvenanceFixtures(t *testing.T) {
	directory := t.TempDir()
	cpuPath := filepath.Join(directory, "cpuinfo")
	memoryPath := filepath.Join(directory, "meminfo")
	if err := os.WriteFile(cpuPath, []byte("processor: 0\nmodel name: Stable Qualification CPU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memoryPath, []byte("MemTotal:       16384 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, err := readCPUModel(cpuPath)
	if err != nil || model != "Stable Qualification CPU" {
		t.Fatalf("readCPUModel() = %q, %v", model, err)
	}
	memory, err := readMemoryBytes(memoryPath)
	if err != nil || memory != 16384*1024 {
		t.Fatalf("readMemoryBytes() = %d, %v", memory, err)
	}
}
