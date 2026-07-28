package sandbox

import (
	"context"
	"testing"
	"time"

	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
)

type fakeExecRPCClient struct {
	lastExec *nodeoperatorv1.ExecRequest
	execResp *nodeoperatorv1.ExecResponse
	execErr  error
}

func (f *fakeExecRPCClient) Exec(request *nodeoperatorv1.ExecRequest) (*nodeoperatorv1.ExecResponse, error) {
	f.lastExec = request
	if f.execResp != nil || f.execErr != nil {
		return f.execResp, f.execErr
	}
	return &nodeoperatorv1.ExecResponse{}, nil
}

func (f *fakeExecRPCClient) ExecStream(time.Duration) (nodeoperatorv1.NodeOperator_ExecStreamClient, context.CancelFunc, error) {
	return nil, func() {}, nil
}

func (f *fakeExecRPCClient) Close() error { return nil }

func TestExecUnaryPassesUser(t *testing.T) {
	fakeClient := &fakeExecRPCClient{}

	if err := execUnary(fakeClient, execOptions{
		timeoutSeconds: 9,
		sandboxID:      "axctl-test",
		command:        []string{"id", "-u"},
		user:           "axern",
	}); err != nil {
		t.Fatalf("execUnary() error = %v", err)
	}

	if fakeClient.lastExec == nil || fakeClient.lastExec.GetSpec() == nil {
		t.Fatal("execUnary() did not send an exec request")
	}
	spec := fakeClient.lastExec.GetSpec()
	if got, want := fakeClient.lastExec.GetSandboxID(), "axctl-test"; got != want {
		t.Fatalf("sandbox id = %q, want %q", got, want)
	}
	if got, want := spec.GetUser(), "axern"; got != want {
		t.Fatalf("spec user = %q, want %q", got, want)
	}
	if got, want := spec.GetTimeoutSeconds(), int64(9); got != want {
		t.Fatalf("timeout seconds = %d, want %d", got, want)
	}
	if got := spec.GetArgv(); len(got) != 2 || got[0] != "id" || got[1] != "-u" {
		t.Fatalf("argv = %v, want [id -u]", got)
	}
}
