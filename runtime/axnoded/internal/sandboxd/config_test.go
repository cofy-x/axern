package sandboxd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestParseConfigUsesTrailingArgsAsEntrypoint(t *testing.T) {
	cfg, err := ParseConfig([]string{
		"--socket", filepath.Join(t.TempDir(), "sandboxd.sock"),
		"--shutdown-timeout", "3s",
		"--", "/bin/sh", "-c", "echo ok",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("shutdown timeout = %v, want 3s", cfg.ShutdownTimeout)
	}
	wantArgs := []string{"/bin/sh", "-c", "echo ok"}
	if !reflect.DeepEqual(cfg.Entrypoint.Args, wantArgs) {
		t.Fatalf("entrypoint args = %#v, want %#v", cfg.Entrypoint.Args, wantArgs)
	}
}

func TestParseConfigLoadsEntrypointJSON(t *testing.T) {
	dir := t.TempDir()
	entrypointPath := filepath.Join(dir, "entrypoint.json")
	if err := os.WriteFile(entrypointPath, []byte(`{"args":["/bin/echo","ok"],"cwd":"/tmp","env":["A=1"]}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig([]string{
		"--socket", filepath.Join(dir, "sandboxd.sock"),
		"--entrypoint-json", entrypointPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if got, want := cfg.Entrypoint.Cwd, "/tmp"; got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(cfg.Entrypoint.Env, []string{"A=1"}) {
		t.Fatalf("env = %#v", cfg.Entrypoint.Env)
	}
}

func TestParseConfigIgnoresFutureEntrypointJSONFields(t *testing.T) {
	dir := t.TempDir()
	entrypointPath := filepath.Join(dir, "entrypoint.json")
	if err := os.WriteFile(entrypointPath, []byte(`{"args":["/bin/echo","ok"],"user":{"uid":1000},"terminal":true}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig([]string{
		"--socket", filepath.Join(dir, "sandboxd.sock"),
		"--entrypoint-json", entrypointPath,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if !reflect.DeepEqual(cfg.Entrypoint.Args, []string{"/bin/echo", "ok"}) {
		t.Fatalf("entrypoint args = %#v", cfg.Entrypoint.Args)
	}
}

func TestParseConfigRejectsAmbiguousEntrypointSources(t *testing.T) {
	dir := t.TempDir()
	entrypointPath := filepath.Join(dir, "entrypoint.json")
	if err := os.WriteFile(entrypointPath, []byte(`{"args":["/bin/echo","ok"]}`), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := ParseConfig([]string{
		"--socket", filepath.Join(dir, "sandboxd.sock"),
		"--entrypoint-json", entrypointPath,
		"--", "/bin/echo", "fallback",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("ParseConfig() error = nil, want ambiguous source error")
	}
}

func TestParseConfigRejectsInvalidEnv(t *testing.T) {
	_, err := ParseConfig([]string{"--", "/bin/echo"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ParseConfig() unexpected error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "entrypoint.json")
	if err := os.WriteFile(path, []byte(`{"args":["/bin/echo"],"env":["BROKEN"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = ParseConfig([]string{"--entrypoint-json", path}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("ParseConfig() error = nil, want invalid env error")
	}
}

func TestParseConfigAllowsEmptyNonExecutableArgs(t *testing.T) {
	cfg, err := ParseConfig([]string{"--", "/bin/echo", ""}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if !reflect.DeepEqual(cfg.Entrypoint.Args, []string{"/bin/echo", ""}) {
		t.Fatalf("entrypoint args = %#v", cfg.Entrypoint.Args)
	}
}

func TestParseConfigRejectsEmptyExecutable(t *testing.T) {
	_, err := ParseConfig([]string{"--", ""}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("ParseConfig() error = nil, want empty executable error")
	}
}
