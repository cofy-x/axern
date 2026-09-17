package sandbox

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	nodeoperatorv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/operator/v1"
	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/urfave/cli"
)

type networkPolicyRPCClient interface {
	ExplainAllocationNetworkPolicy(string) (*nodeoperatorv1.ExplainAllocationNetworkPolicyResponse, error)
	Close() error
}

var newNetworkPolicyRPCClient = func(ctx *cli.Context) (networkPolicyRPCClient, error) {
	return client.New(ctx)
}

var NetworkPolicyCmd = cli.Command{
	Name:  "network-policy",
	Usage: "Explain or check effective Allocation network-policy enforcement",
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
				return fmt.Errorf("exactly one allocation id must be specified")
			}
			opsClient, err := newNetworkPolicyRPCClient(context)
			if err != nil {
				return err
			}
			defer opsClient.Close()
			response, err := opsClient.ExplainAllocationNetworkPolicy(context.Args().First())
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
	AllocationID        string `json:"allocation_id"`
	Mode                string `json:"mode"`
	Status              string `json:"status"`
	CapabilityState     string `json:"capability_state"`
	EnforcementHealthy  bool   `json:"enforcement_healthy"`
	ExactBinding        bool   `json:"exact_binding"`
	EnforcementRevision int64  `json:"enforcement_revision"`
	DomainRuleCount     uint32 `json:"domain_rule_count"`
	CIDRRuleCount       uint32 `json:"cidr_rule_count"`
	PortRangeCount      uint32 `json:"port_range_count"`
	TotalRuleCount      uint32 `json:"total_rule_count"`
}

func renderNetworkPolicyJSON(w io.Writer, response *nodeoperatorv1.ExplainAllocationNetworkPolicyResponse) error {
	if response == nil {
		return fmt.Errorf("network policy diagnostics response is required")
	}
	output := networkPolicyJSON{
		AllocationID: response.GetAllocationID(), Mode: networkPolicyMode(response.GetMode()), Status: networkPolicyStatus(response.GetStatus()),
		CapabilityState: networkPolicyCapabilityState(response.GetCapabilityState()), EnforcementHealthy: response.GetEnforcementHealthy(),
		ExactBinding:        response.GetExactBinding(),
		EnforcementRevision: response.GetEnforcementRevision(), DomainRuleCount: response.GetDomainRuleCount(), CIDRRuleCount: response.GetCidrRuleCount(),
		PortRangeCount: response.GetPortRangeCount(), TotalRuleCount: response.GetTotalRuleCount(),
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(output)
}

func renderNetworkPolicy(w io.Writer, response *nodeoperatorv1.ExplainAllocationNetworkPolicyResponse) {
	if response == nil {
		return
	}
	fmt.Fprintf(w, "Allocation: %s\n", response.GetAllocationID())
	fmt.Fprintf(w, "Mode: %s\n", networkPolicyMode(response.GetMode()))
	fmt.Fprintf(w, "Status: %s\n", networkPolicyStatus(response.GetStatus()))
	fmt.Fprintf(w, "Capability: %s\n", networkPolicyCapabilityState(response.GetCapabilityState()))
	fmt.Fprintf(w, "Enforcement Healthy: %t\n", response.GetEnforcementHealthy())
	fmt.Fprintf(w, "Exact Allocation Binding: %t\n", response.GetExactBinding())
	fmt.Fprintf(w, "Enforcement Revision: %d\n", response.GetEnforcementRevision())
	fmt.Fprintf(w, "Rules: %d total, %d domain, %d CIDR, %d port range\n", response.GetTotalRuleCount(), response.GetDomainRuleCount(), response.GetCidrRuleCount(), response.GetPortRangeCount())
}

func networkPolicyMode(value nodeoperatorv1.AllocationNetworkPolicyMode) string {
	switch value {
	case nodeoperatorv1.AllocationNetworkPolicyMode_ALLOCATION_NETWORK_POLICY_MODE_UNRESTRICTED:
		return "unrestricted"
	case nodeoperatorv1.AllocationNetworkPolicyMode_ALLOCATION_NETWORK_POLICY_MODE_DNS_DENY:
		return "dns_deny"
	case nodeoperatorv1.AllocationNetworkPolicyMode_ALLOCATION_NETWORK_POLICY_MODE_STRICT:
		return "strict"
	default:
		return "unspecified"
	}
}

func networkPolicyStatus(value nodeoperatorv1.AllocationNetworkPolicyStatus) string {
	switch value {
	case nodeoperatorv1.AllocationNetworkPolicyStatus_ALLOCATION_NETWORK_POLICY_STATUS_OK:
		return "ok"
	case nodeoperatorv1.AllocationNetworkPolicyStatus_ALLOCATION_NETWORK_POLICY_STATUS_ABSENT:
		return "absent"
	case nodeoperatorv1.AllocationNetworkPolicyStatus_ALLOCATION_NETWORK_POLICY_STATUS_CAPABILITY_UNAVAILABLE:
		return "capability_unavailable"
	case nodeoperatorv1.AllocationNetworkPolicyStatus_ALLOCATION_NETWORK_POLICY_STATUS_ENFORCEMENT_UNHEALTHY:
		return "enforcement_unhealthy"
	case nodeoperatorv1.AllocationNetworkPolicyStatus_ALLOCATION_NETWORK_POLICY_STATUS_BINDING_MISMATCH:
		return "binding_mismatch"
	default:
		return "unspecified"
	}
}

func networkPolicyCapabilityState(value nodeoperatorv1.AllocationNetworkPolicyCapabilityState) string {
	switch value {
	case nodeoperatorv1.AllocationNetworkPolicyCapabilityState_ALLOCATION_NETWORK_POLICY_CAPABILITY_STATE_AVAILABLE:
		return "available"
	case nodeoperatorv1.AllocationNetworkPolicyCapabilityState_ALLOCATION_NETWORK_POLICY_CAPABILITY_STATE_UNAVAILABLE:
		return "unavailable"
	case nodeoperatorv1.AllocationNetworkPolicyCapabilityState_ALLOCATION_NETWORK_POLICY_CAPABILITY_STATE_UNKNOWN:
		return "unknown"
	case nodeoperatorv1.AllocationNetworkPolicyCapabilityState_ALLOCATION_NETWORK_POLICY_CAPABILITY_STATE_NOT_REQUIRED:
		return "not_required"
	default:
		return "unspecified"
	}
}

func networkPolicyDoctorHealthy(status nodeoperatorv1.AllocationNetworkPolicyStatus) bool {
	return status == nodeoperatorv1.AllocationNetworkPolicyStatus_ALLOCATION_NETWORK_POLICY_STATUS_OK ||
		status == nodeoperatorv1.AllocationNetworkPolicyStatus_ALLOCATION_NETWORK_POLICY_STATUS_ABSENT
}
