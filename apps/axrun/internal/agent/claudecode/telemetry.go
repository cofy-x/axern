package claudecode

import (
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

// commandRecorder wraps a proxy.Recorder to add agent command lifecycle
// events (command_started / command_finished) that are specific to
// harness-level execution, not LLM proxying.
type commandRecorder struct {
	recorder *proxy.Recorder
}

// newCommandRecorder creates a commandRecorder from the recorder provided
// by the rollout layer. Returns (nil, nil) if no recorder is available.
func newCommandRecorder(recorder *proxy.Recorder) *commandRecorder {
	if recorder == nil {
		return nil
	}
	return &commandRecorder{recorder: recorder}
}

func (r *commandRecorder) recordCommandStarted(plan agent.LaunchPlan) {
	if r == nil {
		return
	}
	timestamp := time.Now().UTC()
	commandVector, commandText := commandFields(plan.Command)
	r.recorder.AppendEvent(domain.AgentRawEvent{
		Type:               domain.AgentRawEventCommandStarted,
		Timestamp:          &timestamp,
		LauncherKind:       plan.LauncherKind,
		RuntimeType:        plan.RuntimeType,
		RuntimeImage:       plan.Image,
		RuntimeMountTarget: plan.BundleMountTarget,
		RuntimeBinDir:      agent.AgentBundleBinDir(plan.BundleMountTarget),
		RuntimeProfile:     plan.Profile,
		Command:            commandVector,
		CommandText:        commandText,
		CWD:                plan.CWD,
		User:               plan.User,
		TimeoutSec:         int(plan.Timeout.Seconds()),
	})
}

func (r *commandRecorder) recordCommandFinished(plan agent.LaunchPlan, startedAt time.Time, exitCode *int, err error) {
	if r == nil {
		return
	}
	timestamp := time.Now().UTC()
	commandVector, commandText := commandFields(plan.Command)
	event := domain.AgentRawEvent{
		Type:               domain.AgentRawEventCommandFinished,
		Timestamp:          &timestamp,
		LauncherKind:       plan.LauncherKind,
		RuntimeType:        plan.RuntimeType,
		RuntimeImage:       plan.Image,
		RuntimeMountTarget: plan.BundleMountTarget,
		RuntimeBinDir:      agent.AgentBundleBinDir(plan.BundleMountTarget),
		RuntimeProfile:     plan.Profile,
		Command:            commandVector,
		CommandText:        commandText,
		CWD:                plan.CWD,
		User:               plan.User,
		TimeoutSec:         int(plan.Timeout.Seconds()),
		LatencyMS:          time.Since(startedAt).Milliseconds(),
		ExitCode:           exitCode,
	}
	if err != nil {
		event.Error = err.Error()
	}
	r.recorder.AppendEvent(event)
}

func commandFields(command sandbox.ExecCommand) ([]string, string) {
	if command.Shell() != "" {
		return nil, command.Shell()
	}
	return command.Argv(), ""
}
