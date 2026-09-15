package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	nodenetworkv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/network/v1"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "node-tunneld: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		nodeID        string
		controlTarget string
		networkSocket string
		caCert        string
		cert          string
		cluster       string
		runscBinary   string
		runscRoot     string
		runscIgnoreCG bool
		agentBinary   string
		relayCACert   string
	)
	flag.StringVar(&nodeID, "node-id", os.Getenv("AXERN_NODE_ID"), "node id")
	flag.StringVar(&controlTarget, "control-target", "127.0.0.1:24000", "controld gRPC target")
	flag.StringVar(&networkSocket, "network-socket", "/run/axnoded/network.sock", "local axnoded Allocation network Unix socket")
	flag.StringVar(&caCert, "tls-ca-cert", ".dev/certs/ca.crt", "controld CA certificate")
	flag.StringVar(&cert, "identity-bundle", "/var/lib/axnoded/root/identity/node.pem", "axnoded-owned Node certificate and key bundle (read-only)")
	flag.StringVar(&cluster, "workload-cluster", os.Getenv("AXERN_WORKLOAD_CLUSTER"), "workload URI trust domain")
	flag.StringVar(&runscBinary, "runsc-binary", "/usr/local/bin/runsc", "runsc binary used for runsc tunnel agent exec")
	flag.StringVar(&runscRoot, "runsc-root", "/var/lib/axnoded/root/runsc", "runsc root directory")
	flag.BoolVar(&runscIgnoreCG, "runsc-ignore-cgroups", true, "pass --ignore-cgroups to runsc agent exec")
	flag.StringVar(&agentBinary, "agent-binary", "/usr/local/bin/tunnel-agent", "static tunnel agent binary injected into runsc sandboxes with exec-fd")
	flag.StringVar(&relayCACert, "relay-tls-ca-cert", "", "CA certificate used to verify the tunnel relay")
	flag.Parse()
	if nodeID == "" {
		return fmt.Errorf("node-id is required")
	}
	if _, err := (workloadtls.Identity{Cluster: cluster, Role: "axnoded", NodeID: nodeID}).URI(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	controlConn, err := grpcclient.NewReadyClient(ctx, controlTarget, grpc.WithTransportCredentials(&workloadtls.Credentials{BundlePath: cert, TrustPath: caCert, Local: workloadtls.Identity{Cluster: cluster, Role: "axnoded", NodeID: nodeID}, Peer: workloadtls.Identity{Cluster: cluster, Role: "controld"}}), grpc.WithNoProxy())
	if err != nil {
		return err
	}
	defer controlConn.Close()
	networkConn, err := dialUnix(ctx, networkSocket)
	if err != nil {
		return err
	}
	defer networkConn.Close()
	d := &daemon{
		nodeID:  nodeID,
		node:    nodev1.NewNodeControlClient(controlConn),
		network: nodenetworkv1.NewAllocationNetworkClient(networkConn),
		running: make(map[string]context.CancelFunc),
		runsc: runscConfig{
			binary:        runscBinary,
			root:          runscRoot,
			ignoreCgroups: runscIgnoreCG,
			agentBinary:   agentBinary,
		},
		relay: relayConfig{
			caCert: firstNonEmpty(relayCACert, caCert),
		},
	}
	return d.run(ctx)
}
