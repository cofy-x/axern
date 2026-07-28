package execflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/fileutil"
	"github.com/google/uuid"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

type ProcessState struct {
	processPath string
	dir         string
}

func NewExecProcessState(
	loadSpec func(string) (*spec.Spec, error),
	containerPath func(string) string,
	containerID string,
	requestCommand []string,
	envs map[string]string,
	cwd string,
	user string,
	terminal bool,
) (*ProcessState, error) {
	specConf, err := loadSpec(containerID)
	if err != nil {
		return nil, err
	}
	processConf, err := ExecProcessFromSpec(specConf, requestCommand, envs, cwd, user, terminal)
	if err != nil {
		return nil, err
	}

	execID := uuid.NewString()
	execDir := filepath.Join(containerPath(containerID), "exec", execID)
	if err := os.MkdirAll(execDir, 0755); err != nil {
		return nil, err
	}

	processPath := filepath.Join(execDir, "process.json")
	data, err := json.Marshal(processConf)
	if err != nil {
		return nil, err
	}
	if err := fileutil.AtomicWriteFile(processPath, data, 0600); err != nil {
		return nil, err
	}

	return &ProcessState{
		processPath: processPath,
		dir:         execDir,
	}, nil
}

func (s *ProcessState) ProcessPath() string { return s.processPath }
func (s *ProcessState) StdoutPath() string  { return filepath.Join(s.dir, "stdout.log") }
func (s *ProcessState) StderrPath() string  { return filepath.Join(s.dir, "stderr.log") }

func (s *ProcessState) Cleanup() {
	_ = os.RemoveAll(s.dir)
}

func ExecProcessFromSpec(specConf *spec.Spec, command []string, envs map[string]string, cwd string, user string, terminal bool) (*spec.Process, error) {
	if specConf == nil || specConf.Process == nil {
		return nil, fmt.Errorf("container spec process is not available")
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("exec command can not be empty")
	}

	data, err := json.Marshal(specConf.Process)
	if err != nil {
		return nil, err
	}

	var processConf spec.Process
	if err := json.Unmarshal(data, &processConf); err != nil {
		return nil, err
	}

	processConf.Args = append([]string(nil), command...)
	if cwd != "" {
		processConf.Cwd = cwd
	}
	if user != "" {
		resolved, err := ResolveUserInfo(specConf, user)
		if err != nil {
			return nil, err
		}
		processConf.User = resolved.OCI
		processConf.Env = mergeExecEnv(processConf.Env, resolved.ExecEnv())
	}
	if len(envs) > 0 {
		processConf.Env = mergeExecEnv(processConf.Env, envs)
	}
	processConf.Terminal = terminal
	return &processConf, nil
}

func (u ResolvedUser) ExecEnv() map[string]string {
	out := make(map[string]string, 4)
	if u.Home != "" {
		out["HOME"] = u.Home
	}
	if u.Name != "" {
		out["USER"] = u.Name
		out["LOGNAME"] = u.Name
	}
	if u.Shell != "" {
		out["SHELL"] = u.Shell
	}
	return out
}

func mergeExecEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return append([]string(nil), base...)
	}

	remaining := make(map[string]string, len(overrides))
	for k, v := range overrides {
		remaining[k] = v
	}

	out := make([]string, 0, len(base)+len(overrides))
	for _, raw := range base {
		parts := splitEnvPair(raw)
		if parts[0] == "" {
			out = append(out, raw)
			continue
		}
		if override, ok := remaining[parts[0]]; ok {
			out = append(out, parts[0]+"="+override)
			delete(remaining, parts[0])
			continue
		}
		out = append(out, raw)
	}

	keys := make([]string, 0, len(remaining))
	for key := range remaining {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+remaining[key])
	}
	return out
}

func splitEnvPair(raw string) [2]string {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '=' {
			return [2]string{raw[:i], raw[i+1:]}
		}
	}
	return [2]string{"", raw}
}

func KeyValueMap(values []*runtimeapi.KeyValue) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for _, kv := range values {
		if kv == nil || kv.GetKey() == "" {
			continue
		}
		out[kv.GetKey()] = kv.GetValue()
	}
	return out
}
