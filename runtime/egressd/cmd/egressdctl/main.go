package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "egressdctl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("egressdctl", flag.ContinueOnError)
	socket := flags.String("socket", "/run/egressd/egressd.sock", "egressd Unix socket")
	allocation := flags.String("allocation", "", "allocation ID")
	attempt := flags.Int64("attempt", 0, "allocation attempt")
	ip := flags.String("ip", "", "sandbox source IP")
	revision := flags.Int64("revision", 1, "execution revision")
	mode := flags.String("mode", "", "strict or dns-deny")
	domains := flags.String("domains", "", "comma-separated normalized domain rules")
	upstreams := flags.String("upstreams", "", "comma-separated trusted DNS upstreams")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: egressdctl [flags] health|list|prepare|delete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, closeClient, err := dial(ctx, *socket)
	if err != nil {
		return err
	}
	defer closeClient()
	var output proto.Message
	switch flags.Arg(0) {
	case "health":
		output, err = client.GetEgressManagerHealth(ctx, &runtimeegressv1.EgressManagerHealthRequest{})
	case "list":
		output, err = client.ListPolicies(ctx, &runtimeegressv1.ListPoliciesRequest{AllocationID: *allocation})
	case "delete":
		output, err = client.DeletePolicy(ctx, &runtimeegressv1.DeletePolicyRequest{AllocationID: *allocation, Attempt: *attempt})
	case "prepare":
		policy, policyErr := commandPolicy(*mode, splitCSV(*domains))
		if policyErr != nil {
			return policyErr
		}
		output, err = client.PreparePolicy(ctx, &runtimeegressv1.PreparePolicyRequest{AllocationID: *allocation, Attempt: *attempt, SandboxIp: *ip, Policy: policy, ExecutionRevision: *revision, UpstreamNameservers: splitCSV(*upstreams)})
	default:
		return fmt.Errorf("unknown command %q", flags.Arg(0))
	}
	if err != nil {
		return err
	}
	wire, err := (protojson.MarshalOptions{Multiline: true, Indent: "  ", UseProtoNames: true}).Marshal(output)
	if err != nil {
		return err
	}
	fmt.Println(string(wire))
	return nil
}

func dial(ctx context.Context, socket string) (runtimeegressv1.RuntimeEgressServiceClient, func(), error) {
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", strings.TrimSpace(socket))
	}
	conn, err := grpc.NewClient("unix:"+socket, grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, func() {}, err
	}
	return runtimeegressv1.NewRuntimeEgressServiceClient(conn), func() { _ = conn.Close() }, nil
}

func commandPolicy(mode string, domains []string) (*commonv1.NetworkEgressPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "strict":
		return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{AllowedDomains: domains}}}, nil
	case "dns-deny":
		return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: domains}}}, nil
	default:
		return nil, fmt.Errorf("-mode must be strict or dns-deny, got %s", strconv.Quote(mode))
	}
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
