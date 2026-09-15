package node

import (
	"errors"
	"io"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

func streamOpenError(err error, name string) error {
	if errors.Is(err, io.EOF) {
		return grpcstatus.Errorf(codes.InvalidArgument, "%s stream ended before open", name)
	}
	return err
}

type processClient interface {
	Send(*nodesandboxv1.ProcessRequest) error
	Recv() (*nodesandboxv1.ProcessResponse, error)
	Header() (metadata.MD, error)
	CloseSend() error
}

type uploadArchiveClient interface {
	Send(*nodesandboxv1.UploadArchiveRequest) error
	CloseAndRecv() (*nodesandboxv1.UploadArchiveResponse, error)
	Header() (metadata.MD, error)
	CloseSend() error
}

type downloadArchiveClient interface {
	Recv() (*nodesandboxv1.DownloadArchiveResponse, error)
	Header() (metadata.MD, error)
}

func bridgeProcess(down nodesandboxv1.NodeSandbox_ProcessServer, up processClient) error {
	errCh := make(chan bridgeResult, 2)
	go func() {
		for {
			req, err := down.Recv()
			if errors.Is(err, io.EOF) {
				errCh <- bridgeResult{direction: bridgeDownstream, err: up.CloseSend()}
				return
			}
			if err != nil {
				errCh <- bridgeResult{direction: bridgeDownstream, err: err}
				return
			}
			if err := up.Send(req); err != nil {
				errCh <- bridgeResult{direction: bridgeDownstream, err: err}
				return
			}
		}
	}()
	go func() {
		for {
			resp, err := up.Recv()
			if errors.Is(err, io.EOF) {
				errCh <- bridgeResult{direction: bridgeUpstream}
				return
			}
			if err != nil {
				errCh <- bridgeResult{direction: bridgeUpstream, err: err}
				return
			}
			if err := down.Send(resp); err != nil {
				errCh <- bridgeResult{direction: bridgeUpstream, err: err}
				return
			}
			if resp.GetExit() != nil {
				errCh <- bridgeResult{direction: bridgeUpstream}
				return
			}
		}
	}()
	return firstBridgeResult(errCh)
}

type bridgeDirection string

const (
	bridgeDownstream bridgeDirection = "downstream"
	bridgeUpstream   bridgeDirection = "upstream"
)

type bridgeResult struct {
	direction bridgeDirection
	err       error
}

func firstBridgeResult(errCh <-chan bridgeResult) error {
	result := <-errCh
	if result.err != nil {
		return result.err
	}
	if result.direction == bridgeUpstream {
		return nil
	}
	return (<-errCh).err
}
