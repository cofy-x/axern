package sandbox

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/urfave/cli"
)

type networkPolicyRPCClient interface {
	ExplainSandboxNetworkPolicy(string) (*nodeoperatorv1.ExplainSandboxNetworkPolicyResponse, error)
	Close() error
}

var newNetworkPolicyRPCClient = func(ctx *cli.Context) (networkPolicyRPCClient, error) {
	return client.New(ctx)
}

var NetworkPolicyCmd = cli.Command{
	Name:  "network-policy",
	Usage: "Explain or check effective sandbox network-policy enforcement",
	Subcommands: []cli.Command{
		networkPolicySubcommand("explain", "Explain effective network-policy enforcement", false),
		networkPolicySubcommand("doctor", "Check effective network-policy enforcement health", true),
	},
}

func networkPolicySubcommand(name, usage string, doctor bool) cli.Command {
	return cli.Command{
		Name:  name,
		Usage: usage,
		Flags: []cli.Flag{cli.BoolFlag{Name: "json", Usage: "print stable machine-readable JSON"}},
		Action: func(context *cli.Context) error {
			if context.NArg() != 1 {
				return fmt.Errorf("exactly one sandbox id must be specified")
			}
			opsClient, err := newNetworkPolicyRPCClient(context)
			if err != nil {
				return err
			}
			defer opsClient.Close()
			response, err := opsClient.ExplainSandboxNetworkPolicy(context.Args().First())
			if err != nil {
				return err
			}
			if response == nil {
				return fmt.Errorf("network policy diagnostics returned an empty response")
			}
			if context.Bool("json") {
				err = renderNetworkPolicyJSON(os.Stdout, response)
			} else {
				renderNetworkPolicy(os.Stdout, response)
			}
			if err != nil {
				return err
			}
			if doctor && !networkPolicyDoctorHealthy(response.GetStatus()) {
				return fmt.Errorf("network policy doctor failed: %s", networkPolicyStatus(response.GetStatus()))
			}
			return nil
		},
	}
}

type networkPolicyJSON struct {
	SandboxID             string `json:"sandbox_id"`
	Mode                  string `json:"mode"`
	Status                string `json:"status"`
	CapabilityState       string `json:"capability_state"`
	EnforcementHealthy    bool   `json:"enforcement_healthy"`
	ExactProof            bool   `json:"exact_proof"`
	AllocationAttempt     int64  `json:"allocation_attempt"`
	ExecutionRevision     int64  `json:"execution_revision"`
	EnforcementRevision   int64  `json:"enforcement_revision"`
	DomainRuleCount       uint32 `json:"domain_rule_count"`
	CIDRRuleCount         uint32 `json:"cidr_rule_count"`
	PortRangeCount        uint32 `json:"port_range_count"`
	TotalRuleCount        uint32 `json:"total_rule_count"`
	RecoveredAfterRestart bool   `json:"recovered_after_restart"`
}

func renderNetworkPolicyJSON(w io.Writer, response *nodeoperatorv1.ExplainSandboxNetworkPolicyResponse) error {
	if response == nil {
		return fmt.Errorf("network policy diagnostics response is required")
	}
	output := networkPolicyJSON{
		SandboxID: response.GetSandboxID(), Mode: networkPolicyMode(response.GetMode()), Status: networkPolicyStatus(response.GetStatus()),
		CapabilityState: networkPolicyCapabilityState(response.GetCapabilityState()), EnforcementHealthy: response.GetEnforcementHealthy(),
		ExactProof: response.GetExactProof(), AllocationAttempt: response.GetAllocationAttempt(), ExecutionRevision: response.GetExecutionRevision(),
		EnforcementRevision: response.GetEnforcementRevision(), DomainRuleCount: response.GetDomainRuleCount(), CIDRRuleCount: response.GetCidrRuleCount(),
		PortRangeCount: response.GetPortRangeCount(), TotalRuleCount: response.GetTotalRuleCount(), RecoveredAfterRestart: response.GetRecoveredAfterRestart(),
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(output)
}

func renderNetworkPolicy(w io.Writer, response *nodeoperatorv1.ExplainSandboxNetworkPolicyResponse) {
	if response == nil {
		return
	}
	fmt.Fprintf(w, "Sandbox: %s\n", response.GetSandboxID())
	fmt.Fprintf(w, "Mode: %s\n", networkPolicyMode(response.GetMode()))
	fmt.Fprintf(w, "Status: %s\n", networkPolicyStatus(response.GetStatus()))
	fmt.Fprintf(w, "Capability: %s\n", networkPolicyCapabilityState(response.GetCapabilityState()))
	fmt.Fprintf(w, "Enforcement Healthy: %t\n", response.GetEnforcementHealthy())
	fmt.Fprintf(w, "Exact Proof: %t\n", response.GetExactProof())
	fmt.Fprintf(w, "Allocation Attempt: %d\n", response.GetAllocationAttempt())
	fmt.Fprintf(w, "Execution Revision: %d\n", response.GetExecutionRevision())
	fmt.Fprintf(w, "Enforcement Revision: %d\n", response.GetEnforcementRevision())
	fmt.Fprintf(w, "Rules: %d total, %d domain, %d CIDR, %d port range\n", response.GetTotalRuleCount(), response.GetDomainRuleCount(), response.GetCidrRuleCount(), response.GetPortRangeCount())
	fmt.Fprintf(w, "Recovered After Restart: %t\n", response.GetRecoveredAfterRestart())
}

func networkPolicyMode(value nodeoperatorv1.SandboxNetworkPolicyMode) string {
	switch value {
	case nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_UNRESTRICTED:
		return "unrestricted"
	case nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_DNS_DENY:
		return "dns_deny"
	case nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_STRICT:
		return "strict"
	default:
		return "unspecified"
	}
}

func networkPolicyStatus(value nodeoperatorv1.SandboxNetworkPolicyStatus) string {
	switch value {
	case nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_OK:
		return "ok"
	case nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_ABSENT:
		return "absent"
	case nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_CAPABILITY_UNAVAILABLE:
		return "capability_unavailable"
	case nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_ENFORCEMENT_UNHEALTHY:
		return "enforcement_unhealthy"
	case nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_PROOF_STALE:
		return "proof_stale"
	default:
		return "unspecified"
	}
}

func networkPolicyCapabilityState(value nodeoperatorv1.SandboxNetworkPolicyCapabilityState) string {
	switch value {
	case nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_AVAILABLE:
		return "available"
	case nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_UNAVAILABLE:
		return "unavailable"
	case nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_UNKNOWN:
		return "unknown"
	case nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_NOT_REQUIRED:
		return "not_required"
	default:
		return "unspecified"
	}
}

func networkPolicyDoctorHealthy(status nodeoperatorv1.SandboxNetworkPolicyStatus) bool {
	return status == nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_OK ||
		status == nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_ABSENT
}
