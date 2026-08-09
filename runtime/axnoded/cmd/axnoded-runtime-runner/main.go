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
	monitorInit := flags.Bool("monitor-init", false, "reap the OCI init process created by the runtime")
	readyStatePath := flags.String("ready-state", "", "path to publish create-monitor readiness")
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
	case *monitorInit && *readyStatePath == "":
		fmt.Fprintln(stderr, "ready-state is required in monitor-init mode")
		return 2
	case !*monitorInit && *readyStatePath != "":
		fmt.Fprintln(stderr, "ready-state requires monitor-init mode")
		return 2
	}
	if *monitorInit {
		return runInitMonitor(initMonitorConfig{
			runtimeBinary:  *runtimeBinary,
			runtimeArgs:    runtimeArgs,
			exitStatePath:  *exitStatePath,
			pidFilePath:    *pidFilePath,
			readyStatePath: *readyStatePath,
		}, stderr)
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
	return persistJSONAtomically(path, state, 0644)
}

func persistJSONAtomically(path string, state any, mode os.FileMode) error {
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
	if err := tmpFile.Chmod(mode); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
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
