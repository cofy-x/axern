package verifyutil

import (
	"reflect"
	"testing"
)

func TestStringSliceFlagString(t *testing.T) {
	flag := StringSliceFlag{"a", "b"}
	if got := flag.String(); got != "a,b" {
		t.Fatalf("StringSliceFlag.String() = %q", got)
	}
}

func TestParseUserEnvs(t *testing.T) {
	envs, err := ParseUserEnvs([]string{"A=1", "B=2"})
	if err != nil {
		t.Fatalf("ParseUserEnvs returned error: %v", err)
	}
	want := map[string]string{"A": "1", "B": "2"}
	if !reflect.DeepEqual(envs, want) {
		t.Fatalf("ParseUserEnvs = %#v, want %#v", envs, want)
	}
}

func TestParseUserEnvsRejectsInvalidValue(t *testing.T) {
	if _, err := ParseUserEnvs([]string{"broken"}); err == nil {
		t.Fatal("ParseUserEnvs should reject invalid user env")
	}
}

func TestParseMounts(t *testing.T) {
	mounts, err := ParseMounts([]string{"/tmp/source:/target:ro,rbind"})
	if err != nil {
		t.Fatalf("ParseMounts returned error: %v", err)
	}
	if len(mounts) != 1 || mounts[0].GetSource() != "/tmp/source" || mounts[0].GetTarget() != "/target" {
		t.Fatalf("unexpected mounts: %#v", mounts)
	}
	if !reflect.DeepEqual(mounts[0].Options, []string{"ro", "rbind"}) {
		t.Fatalf("unexpected mount options: %#v", mounts[0].Options)
	}
}

func TestParseMountsRejectsInvalidMount(t *testing.T) {
	if _, err := ParseMounts([]string{"broken"}); err == nil {
		t.Fatal("ParseMounts should reject invalid mount")
	}
}
