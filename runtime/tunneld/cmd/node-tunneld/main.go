package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cofy-x/axern/runtime/tunneld/internal/control"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "node-tunneld: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		nodeID          string
		nodeAuthToken   string
		controlTarget   string
		operatorSocket  string
		insecureControl bool
		caCert          string
		cert            string
		key             string
		runscBinary     string
		runscRoot       string
		runscIgnoreCG   bool
		agentBinary     string
		relayCACert     string
	)
	flag.StringVar(&nodeID, "node-id", os.Getenv("AXERN_NODE_ID"), "node id")
	flag.StringVar(&nodeAuthToken, "node-auth-token", os.Getenv("AXERN_NODE_AUTH_TOKEN"), "node auth token used with node-control tunnel APIs")
	flag.StringVar(&controlTarget, "control-target", "127.0.0.1:24000", "controld gRPC target")
	flag.StringVar(&operatorSocket, "operator-socket", "/run/axnoded/axnoded.sock", "local axnoded operator Unix socket")
	flag.BoolVar(&insecureControl, "insecure-control", false, "connect to controld without TLS")
	flag.StringVar(&caCert, "tls-ca-cert", ".dev/certs/ca.crt", "controld CA certificate")
	flag.StringVar(&cert, "tls-cert", ".dev/certs/client.crt", "client certificate for controld")
	flag.StringVar(&key, "tls-key", ".dev/certs/client.key", "client key for controld")
	flag.StringVar(&runscBinary, "runsc-binary", "/usr/local/bin/runsc", "runsc binary used for runsc tunnel agent exec")
	flag.StringVar(&runscRoot, "runsc-root", "/var/lib/axnoded/root/runsc", "runsc root directory")
	flag.BoolVar(&runscIgnoreCG, "runsc-ignore-cgroups", true, "pass --ignore-cgroups to runsc agent exec")
	flag.StringVar(&agentBinary, "agent-binary", "/usr/local/bin/tunnel-agent", "static tunnel agent binary injected into runsc sandboxes with exec-fd")
	flag.StringVar(&relayCACert, "relay-tls-ca-cert", "", "CA certificate used to verify the tunnel relay")
	flag.Parse()
	if nodeID == "" {
		return fmt.Errorf("node-id is required")
	}
	if nodeAuthToken == "" {
		return fmt.Errorf("node-auth-token is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	controlConn, err := control.Dial(ctx, controlTarget, control.TLSConfig{CACert: caCert, Cert: cert, Key: key}, insecureControl)
	if err != nil {
		return err
	}
	defer controlConn.Close()
	operatorConn, err := dialUnix(ctx, operatorSocket)
	if err != nil {
		return err
	}
	defer operatorConn.Close()
	d := &daemon{
		nodeID:        nodeID,
		nodeAuthToken: nodeAuthToken,
		node:          nodev1.NewNodeControlClient(controlConn),
		operator:      nodeoperatorv1.NewNodeOperatorClient(operatorConn),
		running:       make(map[string]context.CancelFunc),
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
