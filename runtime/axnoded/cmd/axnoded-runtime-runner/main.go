package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("axnoded-runtime-runner", flag.ContinueOnError)
	flags.SetOutput(stderr)

	runtimeBinary := flags.String("runtime-binary", "", "OCI runtime binary to execute")
	exitStatePath := flags.String("exit-state", "", "path to persist runtime exit state")
	pidFilePath := flags.String("pid-file", "", "runtime pid file path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	runtimeArgs := flags.Args()

	switch {
	case *runtimeBinary == "":
		fmt.Fprintln(stderr, "runtime-binary is required")
		return 2
	case *exitStatePath == "":
		fmt.Fprintln(stderr, "exit-state is required")
		return 2
	case *pidFilePath == "":
		fmt.Fprintln(stderr, "pid-file is required")
		return 2
	case len(runtimeArgs) == 0:
		fmt.Fprintln(stderr, "runtime args are required")
		return 2
	}

	cmd := exec.Command(*runtimeBinary, runtimeArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	exitCode := commandExitCode(err)
	if err != nil && exitCode == runtimeStartFailureExitCode {
		fmt.Fprintf(stderr, "run OCI runtime: %v\n", err)
	}

	if persistErr := persistExitState(*exitStatePath, exitState{
		FinishedAt: time.Now().UTC(),
		ExitCode:   exitCode,
	}); persistErr != nil {
		fmt.Fprintf(stderr, "persist runtime exit state: %v\n", persistErr)
	}
	return exitCode
}

const runtimeStartFailureExitCode = 127

type exitState struct {
	ExitCode   int       `json:"exitCode"`
	FinishedAt time.Time `json:"finishedAt"`
}

func persistExitState(path string, state exitState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmpFile.Write(payload); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(0644); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return nil
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitErr.ExitCode()
	}
	return runtimeStartFailureExitCode
}
