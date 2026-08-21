package doctor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	apprun "github.com/cofy-x/axern/apps/cli/internal/application/run"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const dnsProbeScript = `import os, socket, sys
name = os.environ["AXERN_DNS_PROBE_NAME"]
try:
    results = socket.getaddrinfo(name, 443, type=socket.SOCK_STREAM)
except OSError:
    raise SystemExit(20)
if not any(item[0] in (socket.AF_INET, socket.AF_INET6) for item in results):
    raise SystemExit(21)
`

type DNSProbeOptions struct {
	QueryName    string
	TemplateID   string
	RuntimeClass string
	Timeout      time.Duration
	CleanupWait  time.Duration
}

func DNSProbe(ctx context.Context, session *Session, options DNSProbeOptions) Check {
	started := time.Now()
	if session == nil || session.Namespace == nil || session.Secret == nil || session.Environment == nil || session.Run == nil {
		return failedCheck("runtime_dns_sandbox", "runtime_dns_sandbox_probe_failed", "sandbox DNS probe clients are unavailable", "inspect the local CLI installation", started)
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Minute
	}
	if options.CleanupWait <= 0 {
		options.CleanupWait = defaultCleanupWait
	}

	probeCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	namespace, err := dnsProbeNamespace()
	if err != nil {
		return failedCheck("runtime_dns_sandbox", "runtime_dns_sandbox_probe_failed", "sandbox DNS probe could not allocate temporary resource names", "retry the command", started)
	}

	createdNamespace := false
	secretID, environmentID, runID := "", "", ""
	probeCode := ""
	probeMessage := ""

	if _, err := session.Namespace.CreateNamespace(probeCtx, &namespacev1.CreateNamespaceRequest{Namespace: namespace}); err != nil {
		probeCode, probeMessage = "runtime_dns_sandbox_probe_failed", "sandbox DNS probe namespace could not be created"
	} else {
		createdNamespace = true
	}

	if probeCode == "" {
		response, err := session.Secret.CreateSecret(probeCtx, &secretv1.CreateSecretRequest{
			Namespace: namespace,
			Type:      secretv1.SecretType_SECRET_TYPE_OPAQUE,
			StringData: map[string]string{
				"query_name": strings.TrimSpace(options.QueryName),
			},
			Labels: map[string]string{"axern.doctor": "local-dns"},
		})
		if err != nil || response == nil || strings.TrimSpace(response.GetSecret().GetID()) == "" {
			probeCode, probeMessage = "runtime_dns_sandbox_probe_failed", "sandbox DNS probe secret could not be created"
		} else {
			secretID = response.GetSecret().GetID()
		}
	}

	if probeCode == "" {
		response, err := session.Environment.CreateEnvironment(probeCtx, &environmentv1.CreateEnvironmentRequest{
			Spec:   &environmentv1.EnvironmentSpec{Namespace: namespace, TemplateID: strings.TrimSpace(options.TemplateID)},
			Labels: map[string]string{"axern.doctor": "local-dns"},
		})
		if err != nil || response == nil || strings.TrimSpace(response.GetEnvironment().GetID()) == "" {
			probeCode, probeMessage = "runtime_dns_sandbox_probe_failed", "sandbox DNS probe environment could not be created"
		} else {
			environmentID = response.GetEnvironment().GetID()
		}
	}

	if probeCode == "" {
		response, err := session.Run.CreateRun(probeCtx, &runv1.CreateRunRequest{
			Namespace:     namespace,
			EnvironmentID: environmentID,
			Config: &commonv1.ExecutionConfig{
				Argv:         []string{"python", "-c", dnsProbeScript},
				RuntimeClass: strings.TrimSpace(options.RuntimeClass),
				Resources: &commonv1.ResourceSpec{
					Requests: &commonv1.ResourceQuantity{CpuMilli: 50, MemoryBytes: 64 * 1024 * 1024},
					Limits:   &commonv1.ResourceQuantity{CpuMilli: 250},
				},
				SecretEnv: []*commonv1.SecretEnvVar{{Name: "AXERN_DNS_PROBE_NAME", SecretID: secretID, Key: "query_name"}},
			},
			Labels: map[string]string{"axern.doctor": "local-dns"},
		})
		if err != nil || response == nil || strings.TrimSpace(response.GetRun().GetID()) == "" {
			probeCode, probeMessage = "runtime_dns_sandbox_probe_failed", "sandbox DNS probe Run could not be created"
		} else {
			runID = response.GetRun().GetID()
			final, waitErr := apprun.New(session.Run).Wait(probeCtx, runID, apprun.WaitTargetTerminal, options.Timeout, nil)
			switch {
			case final != nil && final.GetExitCodeKnown() && (final.GetExitCode() == 20 || final.GetExitCode() == 21):
				probeCode, probeMessage = "runtime_dns_sandbox_query_failed", "isolated sandbox could not resolve the configured DNS query"
			case waitErr != nil || final == nil || !final.GetExitCodeKnown():
				probeCode, probeMessage = "runtime_dns_sandbox_probe_failed", "sandbox DNS probe did not complete"
			case final.GetExitCode() != 0:
				probeCode, probeMessage = "runtime_dns_sandbox_probe_failed", "sandbox DNS probe exited unexpectedly"
			}
		}
	}

	cleanupErr := cleanupDNSProbe(context.WithoutCancel(ctx), session, namespace, secretID, environmentID, runID, createdNamespace, options.CleanupWait)
	if cleanupErr != nil {
		return failedCheck("runtime_dns_sandbox", "runtime_dns_sandbox_cleanup_failed", "sandbox DNS probe resource cleanup did not complete", "inspect resources associated with the local DNS doctor probe", started)
	}
	if probeCode != "" {
		return failedCheck("runtime_dns_sandbox", probeCode, probeMessage, "inspect `axern local logs node` and verify AXERN_LOCAL_DNS_NAMESERVERS before retrying", started)
	}
	return passedCheck("runtime_dns_sandbox", "runtime_dns_sandbox_resolved", "isolated sandbox resolved the configured DNS query", started)
}

func dnsProbeNamespace() (string, error) {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return "axern-doctor-dns-" + hex.EncodeToString(suffix[:]), nil
}

func cleanupDNSProbe(parent context.Context, session *Session, namespace, secretID, environmentID, runID string, createdNamespace bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var result error
	if runID != "" {
		response, err := session.Run.GetRun(ctx, &runv1.GetRunRequest{RunID: runID})
		if err != nil && grpcstatus.Code(err) != codes.NotFound {
			result = errors.Join(result, err)
		} else if err == nil && !terminalRun(response.GetRun()) {
			if _, cancelErr := session.Run.CancelRun(ctx, &runv1.CancelRunRequest{RunID: runID}); cancelErr != nil && grpcstatus.Code(cancelErr) != codes.NotFound {
				result = errors.Join(result, cancelErr)
			}
		}
	}
	if environmentID != "" {
		response, err := session.Environment.DeleteEnvironment(ctx, &environmentv1.DeleteEnvironmentRequest{EnvironmentID: environmentID})
		if err != nil && grpcstatus.Code(err) != codes.NotFound {
			result = errors.Join(result, err)
		} else if err == nil && (response == nil || response.GetEnvironment().GetStatus() != environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_DELETED) {
			result = errors.Join(result, fmt.Errorf("environment deletion did not reach deleted state"))
		}
	}
	if secretID != "" {
		if _, err := session.Secret.DeleteSecret(ctx, &secretv1.DeleteSecretRequest{SecretID: secretID}); err != nil && grpcstatus.Code(err) != codes.NotFound {
			result = errors.Join(result, err)
		}
	}
	if createdNamespace {
		if _, err := session.Namespace.DeleteNamespace(ctx, &namespacev1.DeleteNamespaceRequest{Namespace: namespace}); err != nil && grpcstatus.Code(err) != codes.NotFound {
			result = errors.Join(result, err)
		}
	}
	return result
}
