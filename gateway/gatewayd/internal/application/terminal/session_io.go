package terminal

import (
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/metadata"
)

type execStream interface {
	Send(*nodesandboxv1.ExecStreamRequest) error
	Recv() (*nodesandboxv1.ExecStreamResponse, error)
	Header() (metadata.MD, error)
	CloseSend() error
}

type Session struct {
	stream execStream
}

type Output struct {
	Stdout []byte
	Stderr []byte
	Exit   *Exit
}

type Exit struct {
	Code    int32
	Message string
}

func (s *Session) Recv() (Output, error) {
	resp, err := s.stream.Recv()
	if err != nil {
		return Output{}, err
	}
	switch payload := resp.GetPayload().(type) {
	case *nodesandboxv1.ExecStreamResponse_Stdout:
		return Output{Stdout: payload.Stdout}, nil
	case *nodesandboxv1.ExecStreamResponse_Stderr:
		return Output{Stderr: payload.Stderr}, nil
	case *nodesandboxv1.ExecStreamResponse_Exit:
		return Output{Exit: &Exit{Code: payload.Exit.GetExitCode(), Message: payload.Exit.GetMessage()}}, nil
	default:
		return Output{}, nil
	}
}

func (s *Session) Write(data []byte) error {
	return s.stream.Send(&nodesandboxv1.ExecStreamRequest{Payload: &nodesandboxv1.ExecStreamRequest_Stdin{Stdin: data}})
}

func (s *Session) Resize(cols, rows uint32) error {
	return s.stream.Send(&nodesandboxv1.ExecStreamRequest{Payload: &nodesandboxv1.ExecStreamRequest_Resize{Resize: &nodesandboxv1.TerminalResize{Cols: cols, Rows: rows}}})
}

func (s *Session) CloseStdin() error {
	return s.stream.Send(&nodesandboxv1.ExecStreamRequest{Payload: &nodesandboxv1.ExecStreamRequest_CloseStdin{CloseStdin: true}})
}

func (s *Session) Close() error {
	return s.stream.CloseSend()
}
