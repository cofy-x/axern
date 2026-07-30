package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	apprun "github.com/cofy-x/axern/apps/cli/internal/application/run"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const defaultCleanupWait = time.Minute

func (c Control) probe(ctx context.Context, session *Session) Check {
	started := time.Now()
	if session.Environment == nil || session.Run == nil {
		return failedCheck("data_plane", "probe_client_missing", "data-plane probe clients are unavailable", "inspect the CLI installation", started)
	}
	options := *c.options.Probe
	if options.CleanupWait <= 0 {
		options.CleanupWait = defaultCleanupWait
	}
	probeCtx := ctx
	cancel := func() {}
	if options.Timeout > 0 {
		probeCtx, cancel = context.WithTimeout(ctx, options.Timeout)
	}
	defer cancel()

	environment, err := session.Environment.CreateEnvironment(probeCtx, &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace:  c.options.Namespace,
			TemplateID: options.TemplateID,
		},
		Labels: map[string]string{"axern.doctor": "probe"},
	})
	if err != nil || strings.TrimSpace(environment.GetEnvironment().GetID()) == "" {
		return failedCheck("data_plane", "probe_environment_create_failed", "probe environment could not be created", "check runtime template availability, image access, namespace quota, and control-plane health", started)
	}
	environmentID := environment.GetEnvironment().GetID()
	runID := ""
	probeErr := error(nil)

	runResponse, err := session.Run.CreateRun(probeCtx, &runv1.CreateRunRequest{
		Namespace:     c.options.Namespace,
		EnvironmentID: environmentID,
		Config: &commonv1.ExecutionConfig{
			Argv:         []string{"python", "-c", "print('axern-doctor-ok')"},
			RuntimeClass: options.RuntimeClass,
			Resources: &commonv1.ResourceSpec{
				Requests: &commonv1.ResourceQuantity{CpuMilli: 50, MemoryBytes: 64 * 1024 * 1024},
				Limits:   &commonv1.ResourceQuantity{CpuMilli: 250, MemoryBytes: 256 * 1024 * 1024},
			},
		},
		Labels: map[string]string{"axern.doctor": "probe"},
	})
	if err != nil || strings.TrimSpace(runResponse.GetRun().GetID()) == "" {
		probeErr = fmt.Errorf("create probe run")
	} else {
		runID = runResponse.GetRun().GetID()
		final, waitErr := apprun.New(session.Run).Wait(probeCtx, runID, apprun.WaitTargetTerminal, options.Timeout, nil)
		if waitErr != nil {
			probeErr = waitErr
		} else if final == nil || !final.GetExitCodeKnown() || final.GetExitCode() != 0 {
			probeErr = fmt.Errorf("probe run did not report a successful exit")
		}
	}

	cleanupErr := cleanupProbe(context.WithoutCancel(ctx), session, runID, environmentID, options.CleanupWait)
	if cleanupErr != nil {
		return failedCheck("data_plane", "probe_cleanup_failed", "probe resource cleanup did not complete", "inspect the probe-labeled Run and Environment resources", started)
	}
	if probeErr != nil {
		return failedCheck("data_plane", "probe_run_failed", "data-plane probe did not complete successfully", "check node readiness, runtime template availability, image access, and namespace quota", started)
	}
	return passedCheck("data_plane", "probe_succeeded", "catalog-backed Run completed and its temporary Environment was deleted", started)
}

func cleanupProbe(parent context.Context, session *Session, runID, environmentID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var result error
	if runID != "" {
		resp, err := session.Run.GetRun(ctx, &runv1.GetRunRequest{RunID: runID})
		if err != nil && grpcstatus.Code(err) != codes.NotFound {
			result = errors.Join(result, err)
		} else if err == nil && !terminalRun(resp.GetRun()) {
			if _, cancelErr := session.Run.CancelRun(ctx, &runv1.CancelRunRequest{RunID: runID}); cancelErr != nil && grpcstatus.Code(cancelErr) != codes.NotFound {
				result = errors.Join(result, cancelErr)
			}
		}
	}
	if environmentID != "" {
		resp, err := session.Environment.DeleteEnvironment(ctx, &environmentv1.DeleteEnvironmentRequest{EnvironmentID: environmentID})
		if err != nil && grpcstatus.Code(err) != codes.NotFound {
			result = errors.Join(result, err)
		} else if err == nil && resp.GetEnvironment().GetStatus() != environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_DELETED {
			result = errors.Join(result, fmt.Errorf("environment deletion did not reach deleted state"))
		}
	}
	return result
}

func terminalRun(run *runv1.Run) bool {
	if run == nil {
		return false
	}
	switch run.GetStatus() {
	case runv1.RunStatus_RUN_STATUS_SUCCEEDED, runv1.RunStatus_RUN_STATUS_FAILED, runv1.RunStatus_RUN_STATUS_CANCELLED:
		return true
	default:
		return false
	}
}
