package runtime

import (
	"context"
	"fmt"
	"sync"
)

type scriptedExecutor struct {
	outputs map[string][][]byte
	errors  map[string][]error
}

func (s *scriptedExecutor) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	key := ""
	for _, candidate := range []string{"wait", "state"} {
		for _, arg := range args {
			if arg == candidate {
				key = candidate
				break
			}
		}
		if key != "" {
			break
		}
	}

	var output []byte
	if seq := s.outputs[key]; len(seq) > 0 {
		output = seq[0]
		s.outputs[key] = seq[1:]
	}

	var err error
	if seq := s.errors[key]; len(seq) > 0 {
		err = seq[0]
		s.errors[key] = seq[1:]
	}

	return output, err
}

type recordingExecutor struct {
	mu          sync.Mutex
	args        [][]string
	ioCalls     int
	stdoutPaths []string
	stderrPaths []string
}

func (r *recordingExecutor) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	recorded := append([]string(nil), args...)
	r.mu.Lock()
	r.args = append(r.args, recorded)
	r.mu.Unlock()
	return nil, nil
}

func (r *recordingExecutor) ExecuteWithIO(ctx context.Context, cmd, _ string, stdoutPath, stderrPath string, args ...string) error {
	r.mu.Lock()
	r.ioCalls++
	r.stdoutPaths = append(r.stdoutPaths, stdoutPath)
	r.stderrPaths = append(r.stderrPaths, stderrPath)
	r.mu.Unlock()
	_, err := r.Execute(ctx, cmd, args...)
	return err
}

func (r *recordingExecutor) IOPaths() (int, []string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ioCalls, append([]string(nil), r.stdoutPaths...), append([]string(nil), r.stderrPaths...)
}

func (r *recordingExecutor) Args() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.args))
	for i := range r.args {
		out[i] = append([]string(nil), r.args[i]...)
	}
	return out
}

type retryingWaitExecutor struct {
	waitCalls int
}

func (e *retryingWaitExecutor) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	for _, arg := range args {
		switch arg {
		case "wait":
			e.waitCalls++
			if e.waitCalls == 1 {
				return nil, fmt.Errorf("wait failed")
			}
			return []byte(`{"status":29}`), nil
		case "state":
			return []byte(`{"status":"stopped"}`), nil
		}
	}
	return nil, nil
}

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}
