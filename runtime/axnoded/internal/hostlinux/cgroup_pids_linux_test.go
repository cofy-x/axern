//go:build linux

package hostlinux

import (
	"os"
	"path/filepath"
	"testing"
)

func pidDomainFixture(t *testing.T) (string, string) {
	t.Helper()
	parent := t.TempDir()
	leaf := filepath.Join(parent, "workload")
	if err := os.Mkdir(leaf, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{parent, leaf} {
		writeFixtureFile(t, dir, "cgroup.type", "domain\n")
		writeFixtureFile(t, dir, "pids.max", "0\n")
	}
	return parent, leaf
}

func TestConfigureCgroupPIDDomainWritesAndVerifiesBothCeilings(t *testing.T) {
	parent, leaf := pidDomainFixture(t)
	if err := configureCgroupPIDDomainDirs(parent, leaf, 1024); err != nil {
		t.Fatal(err)
	}
	if err := verifyCgroupPIDDomainDirs(parent, leaf, 1024); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{parent, leaf} {
		value, err := os.ReadFile(filepath.Join(dir, "pids.max"))
		if err != nil || string(value) != "1024" {
			t.Fatalf("pids.max in %s = %q, error = %v", dir, value, err)
		}
	}
}

func TestVerifyCgroupPIDDomainRejectsChangedOrUnavailableControls(t *testing.T) {
	for _, target := range []string{"parent", "leaf"} {
		for _, value := range []string{"max\n", "1023\n", "0\n", "-1\n", "+1024\n", "01024\n", "1024 1024\n", "", "invalid\n"} {
			t.Run(target+"/"+value, func(t *testing.T) {
				parent, leaf := pidDomainFixture(t)
				writeFixtureFile(t, parent, "pids.max", "1024\n")
				writeFixtureFile(t, leaf, "pids.max", "1024\n")
				dir := parent
				if target == "leaf" {
					dir = leaf
				}
				writeFixtureFile(t, dir, "pids.max", value)
				if err := verifyCgroupPIDDomainDirs(parent, leaf, 1024); err == nil {
					t.Fatal("changed or malformed host task ceiling was accepted")
				}
			})
		}
		for _, control := range []string{"pids.max", "cgroup.type"} {
			for _, unavailable := range []string{"missing", "unreadable"} {
				t.Run(target+"/"+control+"/"+unavailable, func(t *testing.T) {
					parent, leaf := pidDomainFixture(t)
					dir := parent
					if target == "leaf" {
						dir = leaf
					}
					path := filepath.Join(dir, control)
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if unavailable == "unreadable" {
						if err := os.Mkdir(path, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					if err := configureCgroupPIDDomainDirs(parent, leaf, 1024); err == nil {
						t.Fatal("unavailable host task control was accepted")
					}
					if err := verifyCgroupPIDDomainDirs(parent, leaf, 1024); err == nil {
						t.Fatal("unavailable host task control was verified")
					}
				})
			}
		}
	}
}

func TestConfigureCgroupPIDDomainDoesNotWriteBeforeCheckingBothDomains(t *testing.T) {
	parent, leaf := pidDomainFixture(t)
	if err := os.Remove(filepath.Join(leaf, "pids.max")); err != nil {
		t.Fatal(err)
	}
	if err := configureCgroupPIDDomainDirs(parent, leaf, 1024); err == nil {
		t.Fatal("missing leaf controller was accepted")
	}
	value, err := os.ReadFile(filepath.Join(parent, "pids.max"))
	if err != nil || string(value) != "0\n" {
		t.Fatalf("parent was changed before leaf preflight: %q, %v", value, err)
	}
	if _, err := os.Stat(filepath.Join(leaf, "pids.max")); !os.IsNotExist(err) {
		t.Fatalf("missing leaf control was created: %v", err)
	}
}

func TestCgroupPIDDomainRequiresValidPathsAndPositiveLimit(t *testing.T) {
	for _, test := range []struct {
		parent, leaf string
		limit        int64
	}{
		{"", "", 1024}, {"/", "/workload", 1024},
		{"sandbox", "sandbox/workload", 1024},
		{"/sandbox/", "/sandbox/workload", 1024},
		{" /sandbox", " /sandbox/workload", 1024},
		{"/sandbox", "/sandbox/../other", 1024},
		{"/sandbox", "/sandbox/nested/workload", 1024},
		{"/sandbox", "/other/workload", 1024},
		{"/sandbox", "/sandbox", 1024},
		{"/sandbox", "/sandbox/workload", 0},
		{"/sandbox", "/sandbox/workload", -1},
	} {
		if err := ConfigureCgroupPIDDomain(test.parent, test.leaf, test.limit); err == nil {
			t.Fatalf("invalid domain was configured: %+v", test)
		}
		if err := VerifyCgroupPIDDomain(test.parent, test.leaf, test.limit); err == nil {
			t.Fatalf("invalid domain was verified: %+v", test)
		}
	}
	for _, kind := range []string{"threaded", "domain threaded", "domain invalid", ""} {
		parent, leaf := pidDomainFixture(t)
		writeFixtureFile(t, leaf, "cgroup.type", kind)
		if err := configureCgroupPIDDomainDirs(parent, leaf, 1024); err == nil {
			t.Fatalf("non-domain cgroup type %q was accepted", kind)
		}
	}
}
