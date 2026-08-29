//go:build axern_contract

package axernsdk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cofy-x/axern/sdk/go/clientconfig"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type resourceContract struct {
	CPU           []quantityCase `json:"cpu"`
	Memory        []quantityCase `json:"memory"`
	InvalidCPU    []string       `json:"invalid_cpu"`
	InvalidMemory []string       `json:"invalid_memory"`
}

type quantityCase struct {
	Input string `json:"input"`
	Value int64  `json:"value"`
}

type errorContract struct {
	RPC []errorCase `json:"rpc"`
}

type errorCase struct {
	Code      string `json:"code"`
	Number    int    `json:"number"`
	Class     string `json:"class"`
	Retryable bool   `json:"retryable"`
}

type sourceContract struct {
	Valid   []sourceCase `json:"valid"`
	Invalid []sourceCase `json:"invalid"`
}

type sourceCase struct {
	Template    string `json:"template"`
	Image       string `json:"image"`
	Environment string `json:"environment"`
}

type contextContract struct {
	Valid   []contextCase `json:"valid"`
	Invalid []contextCase `json:"invalid"`
}

type contextCase struct {
	Name    string               `json:"name"`
	Context clientconfig.Context `json:"context"`
}

type commonCoreContract struct {
	Client       []string `json:"client"`
	Sandbox      []string `json:"sandbox"`
	AgentSandbox []string `json:"agent_sandbox"`
}

type networkPolicyContract struct {
	Domains struct {
		Input      []string `json:"input"`
		Normalized []string `json:"normalized"`
	} `json:"domains"`
	CIDR struct {
		CIDR     string      `json:"cidr"`
		Protocol string      `json:"protocol"`
		Ports    []PortRange `json:"ports"`
	} `json:"cidr"`
	InvalidDomains []string `json:"invalid_domains"`
	InvalidCIDRs   []string `json:"invalid_cidrs"`
}

func TestSharedResourceContract(t *testing.T) {
	var contract resourceContract
	loadContract(t, "resources.json", &contract)
	for _, item := range contract.CPU {
		got, err := parseCPUQuantity("cpu", ResourceQuantity(item.Input))
		if err != nil || got != item.Value {
			t.Fatalf("parse CPU %q = %d, %v; want %d", item.Input, got, err, item.Value)
		}
	}
	for _, item := range contract.Memory {
		got, err := parseMemoryQuantity("memory", ResourceQuantity(item.Input))
		if err != nil || got != item.Value {
			t.Fatalf("parse memory %q = %d, %v; want %d", item.Input, got, err, item.Value)
		}
	}
	for _, input := range contract.InvalidCPU {
		if _, err := parseCPUQuantity("cpu", ResourceQuantity(input)); !IsValidation(err) {
			t.Fatalf("parse CPU %q error = %v, want validation error", input, err)
		}
	}
	for _, input := range contract.InvalidMemory {
		if _, err := parseMemoryQuantity("memory", ResourceQuantity(input)); !IsValidation(err) {
			t.Fatalf("parse memory %q error = %v, want validation error", input, err)
		}
	}
}

func TestWorkspaceImageContractRejectsAmbiguousOrOverlappingSources(t *testing.T) {
	valid := &WorkspaceImageSource{
		Variants: []WorkspaceImageVariant{
			{Format: "nydus", Image: "example.test/task@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			{Format: "oci", Image: "example.test/task@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		},
		SourcePath: "tasks/task-a/workspace",
		Target:     "/workspace",
	}
	if err := validateWorkspaceImage(valid); err != nil {
		t.Fatalf("valid workspace image: %v", err)
	}
	duplicate := *valid
	duplicate.Variants = []WorkspaceImageVariant{valid.Variants[1], valid.Variants[1]}
	if err := validateWorkspaceImage(&duplicate); !IsValidation(err) {
		t.Fatalf("duplicate formats error = %v", err)
	}
	nested := *valid
	nested.SourcePath = "tasks/group/task-a/workspace"
	if err := validateWorkspaceImage(&nested); !IsValidation(err) {
		t.Fatalf("nested source error = %v", err)
	}
	uppercase := *valid
	uppercase.Variants = append([]WorkspaceImageVariant(nil), valid.Variants...)
	uppercase.Variants[0].Image = "example.test/task@sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := validateWorkspaceImage(&uppercase); !IsValidation(err) {
		t.Fatalf("non-canonical digest error = %v", err)
	}
	if err := validateWorkspaceImageMounts(valid, nil, []VolumeMount{{Name: "workspace", Target: "/workspace/data"}}); !IsValidation(err) {
		t.Fatalf("overlapping volume error = %v", err)
	}
}

func TestSharedErrorContract(t *testing.T) {
	var contract errorContract
	loadContract(t, "errors.json", &contract)
	for _, item := range contract.RPC {
		code := codes.Code(item.Number)
		err := mapRPCError(status.Error(code, item.Code), "contract operation", "alloc-contract")
		if got := ErrorRetryable(err); got != item.Retryable {
			t.Fatalf("ErrorRetryable(%s) = %v, want %v", item.Code, got, item.Retryable)
		}
		matched := map[string]bool{
			"not_found":         IsNotFound(err),
			"permission_denied": IsPermissionDenied(err),
			"timeout":           IsTimeout(err),
			"cancelled":         IsCancelled(err),
			"unavailable":       IsUnavailable(err),
		}
		if !matched[item.Class] {
			t.Fatalf("error %s did not match class %q: %#v", item.Code, item.Class, matched)
		}
	}
}

func TestSharedSandboxSourceContract(t *testing.T) {
	var contract sourceContract
	loadContract(t, "sandbox_sources.json", &contract)
	client := &Client{}
	check := func(item sourceCase) error {
		_, err := NewSandbox(SandboxOptions{
			Client:        client,
			TemplateID:    item.Template,
			Image:         item.Image,
			EnvironmentID: item.Environment,
		})
		return err
	}
	for _, item := range contract.Valid {
		if err := check(item); err != nil {
			t.Fatalf("valid source %#v: %v", item, err)
		}
	}
	for _, item := range contract.Invalid {
		if err := check(item); !IsValidation(err) {
			t.Fatalf("invalid source %#v error = %v, want validation error", item, err)
		}
	}
}

func TestSharedContextContract(t *testing.T) {
	var contract contextContract
	loadContract(t, "contexts.json", &contract)
	for _, item := range contract.Valid {
		if err := clientconfig.Validate(&item.Context); err != nil {
			t.Fatalf("valid context %q: %v", item.Name, err)
		}
	}
	for _, item := range contract.Invalid {
		if err := clientconfig.Validate(&item.Context); err == nil {
			t.Fatalf("invalid context %q was accepted", item.Name)
		}
	}
}

func TestSharedCommonCoreContract(t *testing.T) {
	var contract commonCoreContract
	loadContract(t, "common_core.json", &contract)
	clientMethods := publicMethods(reflect.TypeFor[*Client]())
	sandboxMethods := publicMethods(reflect.TypeFor[*Sandbox]())
	assertContractMethods(t, contract.Client, clientMethods, map[string]string{
		"environment_create": "CreateEnvironment",
		"environment_delete": "DeleteEnvironment",
		"service_create":     "CreateService",
		"service_delete":     "DeleteService",
		"service_replicas":   "ListServiceReplicas",
	})
	assertContractMethods(t, contract.Sandbox, sandboxMethods, map[string]string{
		"lifecycle_start":  "Start",
		"lifecycle_close":  "Close",
		"exec":             "Exec",
		"process":          "Process",
		"file_stat":        "Stat",
		"file_read":        "ReadFile",
		"file_write":       "WriteFile",
		"archive_upload":   "UploadDir",
		"archive_download": "DownloadDir",
		"tunnel":           "OpenTunnel",
	})
	assertContractMethods(t, contract.AgentSandbox, sandboxMethods, map[string]string{
		"capability_status":       "CapabilityStatus",
		"computer_use_status":     "ComputerUseStatus",
		"computer_use_screenshot": "ComputerUseScreenshot",
		"computer_use_display":    "ComputerUseDisplay",
		"computer_use_mouse":      "ComputerUseMouse",
		"computer_use_keyboard":   "ComputerUseKeyboard",
	})
}

func TestSharedNetworkPolicyContract(t *testing.T) {
	var contract networkPolicyContract
	loadContract(t, "network_policies.json", &contract)
	policy, err := DenyDNSNetworkPolicy(contract.Domains.Input...)
	if err != nil {
		t.Fatal(err)
	}
	if got := policy.proto().GetDnsDeny().GetDeniedDomains(); !reflect.DeepEqual(got, contract.Domains.Normalized) {
		t.Fatalf("normalized domains = %v, want %v", got, contract.Domains.Normalized)
	}
	cidrPolicy, err := NewStrictNetworkPolicy(nil, []CIDRRule{{CIDR: contract.CIDR.CIDR, Protocol: EgressProtocol(contract.CIDR.Protocol), Ports: contract.CIDR.Ports}})
	if err != nil || cidrPolicy.proto().GetStrict().GetAllowedCidrs()[0].GetCidr() != contract.CIDR.CIDR {
		t.Fatalf("CIDR policy = %v, %v", cidrPolicy, err)
	}
	for _, value := range contract.InvalidDomains {
		if _, err := AllowDomainNetworkPolicy(value); err == nil {
			t.Fatalf("invalid domain %q succeeded", value)
		}
	}
	for _, value := range contract.InvalidCIDRs {
		if _, err := NewStrictNetworkPolicy(nil, []CIDRRule{{CIDR: value, Protocol: EgressProtocolTCP, Ports: []PortRange{{Start: 443}}}}); err == nil {
			t.Fatalf("invalid CIDR %q succeeded", value)
		}
	}
}

func publicMethods(target reflect.Type) map[string]bool {
	methods := make(map[string]bool, target.NumMethod())
	for index := range target.NumMethod() {
		methods[target.Method(index).Name] = true
	}
	return methods
}

func assertContractMethods(t *testing.T, operations []string, methods map[string]bool, mapping map[string]string) {
	t.Helper()
	for _, operation := range operations {
		method, ok := mapping[operation]
		if !ok {
			t.Fatalf("shared operation %q has no Go SDK mapping", operation)
		}
		if !methods[method] {
			t.Fatalf("Go SDK method %s for shared operation %q is missing", method, operation)
		}
	}
}

func loadContract(t *testing.T, name string, target any) {
	t.Helper()
	path := filepath.Join("..", "contracts", "v1", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
