package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"

	"github.com/cofy-x/axern/apps/cli/internal/command"
)

func TestCreateRejectsMissingStorageBoundsBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{{"--node", "node-a", "--capacity", "513", "--inodes", "64"}, {"--node", "node-a", "--capacity", "1Gi"}, {"--node", "node-a", "--inodes", "64"}, {"--capacity", "1Gi", "--inodes", "64"}, {"--node", "node-a", "--capacity", "1Gi", "--inodes", "0"}} {
		cmd := createCommand(command.Runtime{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("incomplete storage request accepted: %v", args)
		}
	}
}

type waitFailureServer struct {
	workspacev1.UnimplementedWorkspaceControlServer
}

func (waitFailureServer) CreateWorkspace(_ context.Context, req *workspacev1.CreateWorkspaceRequest) (*workspacev1.CreateWorkspaceResponse, error) {
	if req.GetNodeID() == "reject" {
		return nil, status.Error(codes.ResourceExhausted, "no capacity")
	}
	return &workspacev1.CreateWorkspaceResponse{Workspace: &workspacev1.Workspace{ID: "ws-known", Status: workspacev1.WorkspaceStatus_WORKSPACE_STATUS_PROVISIONING}}, nil
}

func (waitFailureServer) GetWorkspace(_ context.Context, _ *workspacev1.GetWorkspaceRequest) (*workspacev1.GetWorkspaceResponse, error) {
	return nil, status.Error(codes.Unavailable, "disconnected during readiness check")
}

func TestCreateRendersAcknowledgedIdentityOnlyAfterAdmission(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.NewServerTLSFromFile(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(creds))
	workspacev1.RegisterWorkspaceControlServer(server, waitFailureServer{})
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	t.Setenv("AXERN_ENDPOINT", listener.Addr().String())
	t.Setenv("AXERN_PROXY_MODE", "direct")
	t.Setenv("AXERN_TLS_SERVER_NAME", "cli.test.example.com")
	t.Setenv("AXERN_TLS_CA_CERT", testdata.Path("x509/server_ca_cert.pem"))
	t.Setenv("AXERN_TLS_CERT", testdata.Path("x509/client1_cert.pem"))
	t.Setenv("AXERN_TLS_KEY", testdata.Path("x509/client1_key.pem"))
	for _, node := range []string{"node-ok", "reject"} {
		t.Run(node, func(t *testing.T) {
			root := &cobra.Command{Use: "axern"}
			cli := createCommand(command.Runtime{Root: root, Options: &command.Options{Output: "json", Timeout: 5 * time.Second}})
			cli.SilenceUsage, cli.SilenceErrors = true, true
			var out bytes.Buffer
			cli.SetOut(&out)
			cli.SetArgs([]string{"--node", node, "--capacity", "1Mi", "--inodes", "64"})
			err := cli.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("expected command failure")
			}
			if node == "reject" {
				if status.Code(err) != codes.ResourceExhausted || out.Len() != 0 {
					t.Fatalf("unconfirmed result %q, error %v", out.String(), err)
				}
				return
			}
			var receipt struct {
				Workspace struct {
					ID     string
					Status string
				}
			}
			if status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), "axern workspace get ws-known") || json.Unmarshal(out.Bytes(), &receipt) != nil || receipt.Workspace.ID != "ws-known" || receipt.Workspace.Status != "provisioning" {
				t.Fatalf("missing creation receipt %q, error %v", out.String(), err)
			}
		})
	}
}
