package nodeclient

import (
	"context"
	"errors"
	"io"
	"sync"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type ProcessEventKind string

const (
	ProcessEventStdout ProcessEventKind = "stdout"
	ProcessEventStderr ProcessEventKind = "stderr"
	ProcessEventExit   ProcessEventKind = "exit"
)

var (
	ErrProcessClosed       = errors.New("sandbox process is closed")
	ErrProcessReadyMissing = errors.New("sandbox process stream ended before ready")
)

type ProcessEvent struct {
	Kind               ProcessEventKind
	Data               []byte
	ExitCode           int32
	Message            string
	ManagedProxyReport *ManagedProxyReport
}

type Process struct {
	stream  nodesandboxv1.NodeSandbox_ProcessClient
	pending []ProcessEvent
	mu      sync.Mutex
	closed  bool
}

type ImageProcess struct {
	stream  nodesandboxv1.NodeSandbox_ProcessImageClient
	pending []ProcessEvent
	mu      sync.Mutex
	closed  bool
}

func (c *Client) Process(ctx context.Context, argv []string, options Options) (*Process, error) {
	stream, err := c.nodes.Process(ctx)
	if err != nil {
		return nil, err
	}
	openRequest := &nodesandboxv1.ProcessRequest{
		Payload: &nodesandboxv1.ProcessRequest_Open{
			Open: &nodesandboxv1.ProcessOpen{
				AllocationID: c.allocationID,
				Spec: &nodesandboxv1.ExecSpec{
					Argv:           append([]string(nil), argv...),
					Env:            cloneMap(options.Env),
					Cwd:            options.Cwd,
					TimeoutSeconds: int64(options.Timeout.Seconds()),
					User:           options.User,
					Tty:            options.TTY,
					ManagedProxy:   managedProxySpec(options.ManagedProxy),
				},
			},
		},
	}
	if err := stream.Send(openRequest); err != nil {
		_ = stream.CloseSend()
		return nil, err
	}
	process := &Process{stream: stream}
	first, err := process.recvInitial()
	if err != nil {
		_ = process.Close()
		if errors.Is(err, io.EOF) {
			return nil, ErrProcessReadyMissing
		}
		return nil, err
	}
	if first.Kind != "" {
		process.pending = append(process.pending, first)
	}
	return process, nil
}

func (c *Client) ProcessImage(ctx context.Context, image string, argv []string, options ImageOptions) (*ImageProcess, error) {
	stream, err := c.nodes.ProcessImage(ctx)
	if err != nil {
		return nil, err
	}
	openRequest := &nodesandboxv1.ProcessImageRequest{
		Payload: &nodesandboxv1.ProcessImageRequest_Open{
			Open: &nodesandboxv1.ProcessImageOpen{
				AllocationID: c.allocationID,
				Spec:         imageProcessSpec(image, argv, options),
			},
		},
	}
	if err := stream.Send(openRequest); err != nil {
		_ = stream.CloseSend()
		return nil, err
	}
	process := &ImageProcess{stream: stream}
	first, err := process.recvInitial()
	if err != nil {
		_ = process.Close()
		if errors.Is(err, io.EOF) {
			return nil, ErrProcessReadyMissing
		}
		return nil, err
	}
	if first.Kind != "" {
		process.pending = append(process.pending, first)
	}
	return process, nil
}

func (p *Process) Write(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrProcessClosed
	}
	return p.stream.Send(&nodesandboxv1.ProcessRequest{
		Payload: &nodesandboxv1.ProcessRequest_Stdin{Stdin: append([]byte(nil), data...)},
	})
}

func (p *ImageProcess) Write(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrProcessClosed
	}
	return p.stream.Send(&nodesandboxv1.ProcessImageRequest{
		Payload: &nodesandboxv1.ProcessImageRequest_Stdin{Stdin: append([]byte(nil), data...)},
	})
}

func (p *Process) CloseStdin() error {
	return p.send(&nodesandboxv1.ProcessRequest{Payload: &nodesandboxv1.ProcessRequest_CloseStdin{CloseStdin: true}})
}

func (p *ImageProcess) CloseStdin() error {
	return p.send(&nodesandboxv1.ProcessImageRequest{Payload: &nodesandboxv1.ProcessImageRequest_CloseStdin{CloseStdin: true}})
}

func (p *Process) Resize(cols, rows uint32) error {
	return p.send(&nodesandboxv1.ProcessRequest{
		Payload: &nodesandboxv1.ProcessRequest_Resize{Resize: &nodesandboxv1.TerminalResize{Cols: cols, Rows: rows}},
	})
}

func (p *ImageProcess) Resize(cols, rows uint32) error {
	return p.send(&nodesandboxv1.ProcessImageRequest{
		Payload: &nodesandboxv1.ProcessImageRequest_Resize{Resize: &nodesandboxv1.TerminalResize{Cols: cols, Rows: rows}},
	})
}

func (p *Process) Signal(signal string) error {
	return p.send(&nodesandboxv1.ProcessRequest{
		Payload: &nodesandboxv1.ProcessRequest_Signal{Signal: &nodesandboxv1.ProcessSignal{Signal: signal}},
	})
}

func (p *ImageProcess) Signal(signal string) error {
	return p.send(&nodesandboxv1.ProcessImageRequest{
		Payload: &nodesandboxv1.ProcessImageRequest_Signal{Signal: &nodesandboxv1.ProcessSignal{Signal: signal}},
	})
}

func (p *Process) Recv() (ProcessEvent, error) {
	p.mu.Lock()
	if len(p.pending) > 0 {
		event := p.pending[0]
		p.pending = p.pending[1:]
		p.mu.Unlock()
		return event, nil
	}
	p.mu.Unlock()
	return p.recv()
}

func (p *ImageProcess) Recv() (ProcessEvent, error) {
	p.mu.Lock()
	if len(p.pending) > 0 {
		event := p.pending[0]
		p.pending = p.pending[1:]
		p.mu.Unlock()
		return event, nil
	}
	p.mu.Unlock()
	return p.recv()
}

func (p *Process) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	stream := p.stream
	p.mu.Unlock()
	if stream != nil {
		return stream.CloseSend()
	}
	return nil
}

func (p *ImageProcess) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	stream := p.stream
	p.mu.Unlock()
	if stream != nil {
		return stream.CloseSend()
	}
	return nil
}

func (p *Process) send(request *nodesandboxv1.ProcessRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrProcessClosed
	}
	return p.stream.Send(request)
}

func (p *ImageProcess) send(request *nodesandboxv1.ProcessImageRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrProcessClosed
	}
	return p.stream.Send(request)
}

func (p *Process) recv() (ProcessEvent, error) {
	for {
		response, err := p.stream.Recv()
		if err != nil {
			return ProcessEvent{}, err
		}
		event, ready := processEvent(response)
		if ready || event.Kind == "" {
			continue
		}
		return event, nil
	}
}

func (p *ImageProcess) recv() (ProcessEvent, error) {
	for {
		response, err := p.stream.Recv()
		if err != nil {
			return ProcessEvent{}, err
		}
		event, ready := imageProcessEvent(response)
		if ready || event.Kind == "" {
			continue
		}
		return event, nil
	}
}

func (p *Process) recvInitial() (ProcessEvent, error) {
	response, err := p.stream.Recv()
	if err != nil {
		return ProcessEvent{}, err
	}
	event, _ := processEvent(response)
	return event, nil
}

func (p *ImageProcess) recvInitial() (ProcessEvent, error) {
	response, err := p.stream.Recv()
	if err != nil {
		return ProcessEvent{}, err
	}
	event, _ := imageProcessEvent(response)
	return event, nil
}

func processEvent(response *nodesandboxv1.ProcessResponse) (ProcessEvent, bool) {
	switch payload := response.GetPayload().(type) {
	case *nodesandboxv1.ProcessResponse_Ready:
		return ProcessEvent{}, true
	case *nodesandboxv1.ProcessResponse_Stdout:
		return ProcessEvent{Kind: ProcessEventStdout, Data: append([]byte(nil), payload.Stdout...)}, false
	case *nodesandboxv1.ProcessResponse_Stderr:
		return ProcessEvent{Kind: ProcessEventStderr, Data: append([]byte(nil), payload.Stderr...)}, false
	case *nodesandboxv1.ProcessResponse_Exit:
		exit := payload.Exit
		return ProcessEvent{
			Kind:               ProcessEventExit,
			ExitCode:           exit.GetExitCode(),
			Message:            exit.GetMessage(),
			ManagedProxyReport: managedProxyReport(exit.GetManagedProxyReport()),
		}, false
	default:
		return ProcessEvent{}, false
	}
}

func imageProcessEvent(response *nodesandboxv1.ProcessImageResponse) (ProcessEvent, bool) {
	switch payload := response.GetPayload().(type) {
	case *nodesandboxv1.ProcessImageResponse_Ready:
		return ProcessEvent{}, true
	case *nodesandboxv1.ProcessImageResponse_Stdout:
		return ProcessEvent{Kind: ProcessEventStdout, Data: append([]byte(nil), payload.Stdout...)}, false
	case *nodesandboxv1.ProcessImageResponse_Stderr:
		return ProcessEvent{Kind: ProcessEventStderr, Data: append([]byte(nil), payload.Stderr...)}, false
	case *nodesandboxv1.ProcessImageResponse_Exit:
		exit := payload.Exit
		return ProcessEvent{
			Kind:               ProcessEventExit,
			ExitCode:           exit.GetExitCode(),
			Message:            exit.GetMessage(),
			ManagedProxyReport: managedProxyReport(exit.GetManagedProxyReport()),
		}, false
	default:
		return ProcessEvent{}, false
	}
}
