package doctor

import (
	"context"
	"fmt"
	"strings"
	"time"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Control struct {
	options Options
}

func New(options Options) Control {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.CheckTimeout <= 0 {
		options.CheckTimeout = 15 * time.Second
	}
	return Control{options: options}
}

func ConfigurationFailure(contextName, namespace string, probe bool) Report {
	report := newReport(contextName, namespace, probe)
	report.add(failedCheck(
		"configuration",
		"connection_configuration_invalid",
		"connection settings are incomplete or invalid",
		"select a valid context or provide a complete explicit mTLS connection",
		time.Now(),
	))
	for _, name := range []string{"tls_material", "tls_expiry", "tls_key_permissions", "gateway", "namespace", "catalog"} {
		report.add(skippedCheck(name, "configuration validation did not pass"))
	}
	if probe {
		report.add(skippedCheck("data_plane", "configuration validation did not pass"))
	}
	return report
}

func (c Control) Diagnose(ctx context.Context) Report {
	report := newReport(c.options.ContextName, c.options.Namespace, c.options.Probe != nil)
	report.add(Check{Name: "configuration", Status: CheckPass, Code: "connection_configuration_valid", Message: "connection settings resolved"})

	tlsChecks := inspectTLS(c.options.TLS, c.options.Now())
	tlsFailed := false
	for _, check := range tlsChecks {
		report.add(check)
		tlsFailed = tlsFailed || check.Status == CheckFail
	}
	if tlsFailed {
		for _, name := range []string{"gateway", "namespace", "catalog"} {
			report.add(skippedCheck(name, "mTLS validation did not pass"))
		}
		if c.options.Probe != nil {
			report.add(skippedCheck("data_plane", "mTLS validation did not pass"))
		}
		return report
	}

	started := time.Now()
	if c.options.Open == nil {
		report.add(failedCheck("gateway", "gateway_opener_missing", "gateway connection is unavailable", "reinstall the CLI and retry", started))
		skipGatewayDependents(&report, c.options.Probe != nil)
		return report
	}
	session, err := c.options.Open(ctx)
	if err != nil {
		code, message, remediation := connectionFailure(err)
		report.add(failedCheck("gateway", code, message, remediation, started))
		skipGatewayDependents(&report, c.options.Probe != nil)
		return report
	}
	if session == nil {
		report.add(failedCheck("gateway", "gateway_session_missing", "gateway connection returned no session", "retry the command and inspect gateway logs", started))
		skipGatewayDependents(&report, c.options.Probe != nil)
		return report
	}
	if session.Close != nil {
		defer session.Close()
	}
	requestContext := session.Context
	if requestContext == nil {
		requestContext = ctx
	}
	report.add(passedCheck("gateway", "gateway_reachable", "gateway mTLS and gRPC connection succeeded", started))

	namespaceOK := c.checkNamespace(requestContext, &report, session.Namespace)
	c.checkCatalog(requestContext, &report, session.Catalog)
	if c.options.Probe != nil {
		if !namespaceOK {
			report.add(skippedCheck("data_plane", "namespace validation did not pass"))
		} else {
			report.add(c.probe(requestContext, session))
		}
	}
	return report
}

func newReport(contextName, namespace string, probe bool) Report {
	mode := "read_only"
	if probe {
		mode = "probe"
	}
	return Report{
		Status:    StatusHealthy,
		Context:   strings.TrimSpace(contextName),
		Namespace: strings.TrimSpace(namespace),
		Mode:      mode,
		Checks:    []Check{},
	}
}

func skipGatewayDependents(report *Report, probe bool) {
	for _, name := range []string{"namespace", "catalog"} {
		report.add(skippedCheck(name, "gateway connection did not pass"))
	}
	if probe {
		report.add(skippedCheck("data_plane", "gateway connection did not pass"))
	}
}

func (c Control) checkNamespace(ctx context.Context, report *Report, client NamespaceClient) bool {
	started := time.Now()
	if client == nil {
		report.add(failedCheck("namespace", "namespace_client_missing", "namespace API is unavailable", "inspect the CLI installation", started))
		return false
	}
	checkCtx, cancel := context.WithTimeout(ctx, c.options.CheckTimeout)
	defer cancel()
	resp, err := client.GetNamespace(checkCtx, &namespacev1.GetNamespaceRequest{Namespace: c.options.Namespace})
	if err != nil {
		code := "namespace_unavailable"
		message := "namespace API request failed"
		remediation := "check gateway authorization and control-plane health"
		if grpcstatus.Code(err) == codes.NotFound {
			code = "namespace_not_found"
			message = "the selected namespace does not exist"
			remediation = "select or create the namespace before running workloads"
		}
		report.add(failedCheck("namespace", code, message, remediation, started))
		return false
	}
	if resp.GetNamespace() == nil {
		report.add(failedCheck("namespace", "namespace_response_invalid", "namespace API returned an empty resource", "inspect control-plane health", started))
		return false
	}
	report.add(passedCheck("namespace", "namespace_reachable", "selected namespace is accessible", started))
	return true
}

func (c Control) checkCatalog(ctx context.Context, report *Report, client CatalogClient) {
	started := time.Now()
	if client == nil {
		report.add(failedCheck("catalog", "catalog_client_missing", "runtime catalog API is unavailable", "inspect the CLI installation", started))
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, c.options.CheckTimeout)
	defer cancel()
	resp, err := client.ListRuntimeTemplates(checkCtx, &catalogv1.ListRuntimeTemplatesRequest{})
	if err != nil {
		report.add(failedCheck("catalog", "catalog_unavailable", "runtime catalog API request failed", "check gateway authorization and control-plane health", started))
		return
	}
	report.add(passedCheck("catalog", "catalog_reachable", fmt.Sprintf("runtime catalog is accessible (%d templates)", len(resp.GetRuntimeTemplates())), started))
}

func connectionFailure(err error) (string, string, string) {
	switch grpcstatus.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return "gateway_authentication_failed", "gateway rejected the client identity", "rotate or re-import the context credentials"
	case codes.DeadlineExceeded:
		return "gateway_timeout", "gateway connection timed out", "check network reachability, proxy policy, and gateway readiness"
	default:
		if err == context.DeadlineExceeded {
			return "gateway_timeout", "gateway connection timed out", "check network reachability, proxy policy, and gateway readiness"
		}
		return "gateway_unreachable", "gateway mTLS or gRPC connection failed", "check context credentials, network reachability, proxy policy, and gateway readiness"
	}
}
