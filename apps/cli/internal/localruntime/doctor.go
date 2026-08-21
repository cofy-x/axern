package localruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	doctorHealthy  = "healthy"
	doctorDegraded = "degraded"
	doctorFailed   = "failed"

	checkPass = "pass"
	checkWarn = "warn"
	checkFail = "fail"
	checkSkip = "skip"
)

type doctorDNSConfigSource int

const (
	doctorDNSConfigApplied doctorDNSConfigSource = iota
	doctorDNSConfigDesired
)

type nodeDNSProbeResult struct {
	Status                  string `json:"status"`
	Code                    string `json:"code"`
	ConfiguredResolverCount int64  `json:"configured_resolver_count"`
	SuccessfulResolverCount int64  `json:"successful_resolver_count"`
}

func newDoctorReport(probe bool) DoctorReport {
	mode := "read_only"
	if probe {
		mode = "probe"
	}
	return DoctorReport{Status: doctorHealthy, Mode: mode, Checks: []Check{}}
}

func (r *DoctorReport) add(check Check) {
	r.Checks = append(r.Checks, check)
	switch check.Status {
	case checkFail:
		r.Status = doctorFailed
	case checkWarn:
		if r.Status == doctorHealthy {
			r.Status = doctorDegraded
		}
	}
}

func doctorCheck(name, status, code, message, remediation string, started time.Time, details map[string]int64) Check {
	duration := int64(0)
	if !started.IsZero() {
		duration = time.Since(started).Milliseconds()
	}
	return Check{Name: name, Status: status, Code: code, DurationMS: duration, Message: message, Remediation: remediation, Details: details}
}

func skippedDoctorCheck(name, code, message string) Check {
	return doctorCheck(name, checkSkip, code, message, "", time.Time{}, nil)
}

func (m *Manager) doctorDNSNameservers(source doctorDNSConfigSource) ([]string, error) {
	if source == doctorDNSConfigDesired {
		return localDNSNameservers()
	}
	if _, err := loadMetadata(m.metadataPath()); err == nil {
		return readMaterializedDNSNameservers(m.envPath())
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read local metadata: %w", err)
	}
	return localDNSNameservers()
}

func readMaterializedDNSNameservers(path string) ([]string, error) {
	value, err := readMaterializedEnvValue(path, "AXNODED_DNS_NAMESERVERS")
	if err != nil {
		return nil, err
	}
	return discoverLocalDNSNameservers(value, nil)
}

func readMaterializedEnvValue(path, target string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read materialized local configuration: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != target {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, unquoteErr := strconv.Unquote(value); unquoteErr == nil {
			value = unquoted
		}
		return value, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan materialized local configuration: %w", err)
	}
	return "", fmt.Errorf("materialized local configuration is missing %s", target)
}

func (m *Manager) doctorNodeID() string {
	value, err := readMaterializedEnvValue(m.envPath(), "AXNODED_CONTROL_PLANE_NODE_ID")
	if err != nil || strings.TrimSpace(value) == "" {
		return LocalNodeID
	}
	return strings.TrimSpace(value)
}

func NormalizeDNSQueryName(value string) (string, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "."))
	if value == "" || len(value) > 253 {
		return "", fmt.Errorf("DNS query name is invalid")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("DNS query name is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return "", fmt.Errorf("DNS query name is invalid")
			}
		}
	}
	return value + ".", nil
}

func (m *Manager) probeNodeDNS(ctx context.Context, profile, queryName string, timeout time.Duration) Check {
	started := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := m.composeOutput(probeCtx, profile,
		"exec", "-T",
		"-e", "AXERN_DNS_PROBE_NAME="+queryName,
		"-e", "AXERN_DNS_PROBE_TIMEOUT="+timeout.String(),
		"node", "/usr/local/libexec/axnoded/dns-probe",
	)
	if err != nil {
		return doctorCheck("runtime_dns_node", checkFail, "runtime_dns_node_unreachable", "Node container DNS probe could not run", "inspect `axern local logs node` and recreate the local stack", started, nil)
	}
	var result nodeDNSProbeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return doctorCheck("runtime_dns_node", checkFail, "runtime_dns_node_unreachable", "Node container DNS probe returned an invalid result", "upgrade or recreate the local stack", started, nil)
	}
	details := map[string]int64{
		"configured_resolver_count": result.ConfiguredResolverCount,
		"successful_resolver_count": result.SuccessfulResolverCount,
	}
	switch {
	case result.Status == checkPass && result.Code == "runtime_dns_node_reachable" && result.SuccessfulResolverCount == result.ConfiguredResolverCount && result.ConfiguredResolverCount > 0:
		return doctorCheck("runtime_dns_node", checkPass, "runtime_dns_node_reachable", "all configured resolvers answered from the Node container", "", started, details)
	case result.Status == checkWarn && result.Code == "runtime_dns_node_partial" && result.SuccessfulResolverCount > 0 && result.SuccessfulResolverCount < result.ConfiguredResolverCount:
		return doctorCheck("runtime_dns_node", checkWarn, "runtime_dns_node_partial", "only part of the configured resolver set answered from the Node container", "remove or repair unreachable resolvers and recreate the local stack", started, details)
	case result.Status == checkFail && result.Code == "runtime_dns_node_unreachable" && result.SuccessfulResolverCount == 0 && result.ConfiguredResolverCount > 0:
		return doctorCheck("runtime_dns_node", checkFail, "runtime_dns_node_unreachable", "configured resolvers did not answer from the Node container", "set AXERN_LOCAL_DNS_NAMESERVERS to reachable resolver IPs, then run `axern local down` and `axern local up`", started, details)
	default:
		return doctorCheck("runtime_dns_node", checkFail, "runtime_dns_node_unreachable", "Node container DNS probe returned an inconsistent result", "upgrade or recreate the local stack", started, nil)
	}
}
