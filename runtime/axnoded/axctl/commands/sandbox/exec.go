package sandbox

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/urfave/cli"
	"golang.org/x/term"
)

type execRPCClient interface {
	Exec(request *nodeoperatorv1.ExecRequest) (*nodeoperatorv1.ExecResponse, error)
	ExecStream(timeout time.Duration) (nodeoperatorv1.NodeOperator_ExecStreamClient, context.CancelFunc, error)
	Close() error
}

var newExecRPCClient = func(ctx *cli.Context) (execRPCClient, error) {
	return client.New(ctx)
}

type execOptions struct {
	timeoutSeconds int64
	sandboxID      string
	command        []string
	user           string
}

var ExecCmd = cli.Command{
	Name:  "exec",
	Usage: "Execute a command in a sandbox on the current node",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "interactive, i",
			Usage: "Keep stdin open for interactive exec",
		},
		cli.BoolFlag{
			Name:  "tty, t",
			Usage: "Allocate a pseudo-TTY for interactive exec",
		},
		cli.StringFlag{
			Name:  "user, u",
			Usage: "Container user for exec, for example axern or 1000:1000",
		},
	},
	Action: func(context *cli.Context) error {
		if context.NArg() < 2 {
			return fmt.Errorf("usage: axctl sandbox exec <sandbox_id> -- command [args...]")
		}
		if context.Bool("interactive") != context.Bool("tty") {
			return fmt.Errorf("interactive exec requires both -i and -t")
		}

		opsClient, err := newExecRPCClient(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		timeoutSeconds := execTimeoutSeconds(context)
		sandboxID := context.Args().First()
		command := context.Args().Tail()
		if len(command) > 0 && command[0] == "--" {
			command = command[1:]
		}
		if len(command) == 0 {
			return fmt.Errorf("usage: axctl sandbox exec <sandbox_id> -- command [args...]")
		}
		opts := execOptions{
			timeoutSeconds: timeoutSeconds,
			sandboxID:      sandboxID,
			command:        command,
			user:           strings.TrimSpace(context.String("user")),
		}

		if context.Bool("interactive") {
			return execInteractive(opsClient, interactiveExecStreamTimeout(context), opts)
		}
		return execUnary(opsClient, opts)
	},
}

func execUnary(client execRPCClient, opts execOptions) error {
	resp, err := client.Exec(&nodeoperatorv1.ExecRequest{
		SandboxID: opts.sandboxID,
		Spec:      opts.spec(false),
	})
	if err != nil {
		return err
	}

	if len(resp.GetStdout()) > 0 {
		if _, err := os.Stdout.Write(resp.GetStdout()); err != nil {
			return err
		}
	}
	if len(resp.GetStderr()) > 0 {
		if _, err := os.Stderr.Write(resp.GetStderr()); err != nil {
			return err
		}
	}
	if resp.GetStdoutTruncated() {
		fmt.Fprintln(os.Stderr, "warning: exec stdout truncated to 1 MiB")
	}
	if resp.GetStderrTruncated() {
		fmt.Fprintln(os.Stderr, "warning: exec stderr truncated to 1 MiB")
	}
	if resp.GetExitCode() == 0 {
		return nil
	}
	return cli.NewExitError("", int(resp.GetExitCode()))
}

func (o execOptions) spec(tty bool) *nodesandboxv1.ExecSpec {
	return &nodesandboxv1.ExecSpec{
		Argv:           o.command,
		Tty:            tty,
		TimeoutSeconds: o.timeoutSeconds,
		User:           o.user,
	}
}

func interactiveExecStreamTimeout(ctx *cli.Context) time.Duration {
	if !ctx.GlobalIsSet("timeout") {
		return 0
	}
	return ctx.GlobalDuration("timeout")
}

func execTimeoutSeconds(ctx *cli.Context) int64 {
	if !ctx.GlobalIsSet("timeout") {
		return 0
	}
	return int64(math.Ceil(ctx.GlobalDuration("timeout").Seconds()))
}

func execInteractive(client execRPCClient, streamTimeout time.Duration, opts execOptions) error {
	stream, cancel, err := client.ExecStream(streamTimeout)
	if err != nil {
		return err
	}
	defer cancel()

	sendMu := sync.Mutex{}
	send := func(req *nodeoperatorv1.ExecStreamRequest) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(req)
	}

	if err := send(&nodeoperatorv1.ExecStreamRequest{
		Payload: &nodeoperatorv1.ExecStreamRequest_Open{Open: &nodeoperatorv1.ExecStreamOpen{
			SandboxID:   opts.sandboxID,
			Spec:        opts.spec(true),
			InitialSize: terminalResizeFromFD(int(os.Stdin.Fd())),
		}},
	}); err != nil {
		return err
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	interrupted := false
	go func() {
		<-signalCh
		interrupted = true
		cancel()
	}()

	restoreTerm, err := configureLocalTTY(send)
	if err != nil {
		return err
	}
	defer restoreTerm()

	inputErrCh := make(chan error, 1)
	go func() {
		defer close(inputErrCh)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := os.Stdin.Read(buf)
			if n > 0 {
				if err := send(&nodeoperatorv1.ExecStreamRequest{
					Payload: &nodeoperatorv1.ExecStreamRequest_Stdin{Stdin: append([]byte(nil), buf[:n]...)},
				}); err != nil {
					inputErrCh <- err
					return
				}
			}
			if readErr == nil {
				continue
			}
			if readErr == io.EOF {
				inputErrCh <- send(&nodeoperatorv1.ExecStreamRequest{
					Payload: &nodeoperatorv1.ExecStreamRequest_CloseStdin{CloseStdin: true},
				})
				return
			}
			inputErrCh <- readErr
			return
		}
	}()

	for {
		resp, err := stream.Recv()
		if err != nil {
			select {
			case inputErr := <-inputErrCh:
				if inputErr != nil && inputErr != io.EOF {
					return inputErr
				}
			default:
			}
			if interrupted {
				return cli.NewExitError("", 130)
			}
			return err
		}

		switch payload := resp.Payload.(type) {
		case *nodeoperatorv1.ExecStreamResponse_Stdout:
			if _, err := os.Stdout.Write(payload.Stdout); err != nil {
				return err
			}
		case *nodeoperatorv1.ExecStreamResponse_Stderr:
			if _, err := os.Stderr.Write(payload.Stderr); err != nil {
				return err
			}
		case *nodeoperatorv1.ExecStreamResponse_Exit:
			if payload.Exit.GetExitCode() == 0 {
				return nil
			}
			return cli.NewExitError("", int(payload.Exit.GetExitCode()))
		}
	}
}

func configureLocalTTY(send func(*nodeoperatorv1.ExecStreamRequest) error) (func(), error) {
	stdinFD := int(os.Stdin.Fd())
	if !term.IsTerminal(stdinFD) {
		return func() {}, nil
	}
	oldState, err := term.MakeRaw(stdinFD)
	if err != nil {
		return nil, err
	}
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		defer signal.Stop(sigwinch)
		for {
			select {
			case <-done:
				return
			case <-sigwinch:
				if req := terminalResizeRequest(stdinFD); req != nil {
					_ = send(req)
				}
			}
		}
	}()

	return func() {
		close(done)
		_ = term.Restore(stdinFD, oldState)
	}, nil
}

func terminalResizeRequest(fd int) *nodeoperatorv1.ExecStreamRequest {
	size := terminalResizeFromFD(fd)
	if size == nil {
		return nil
	}
	return &nodeoperatorv1.ExecStreamRequest{
		Payload: &nodeoperatorv1.ExecStreamRequest_Resize{Resize: size},
	}
}

func terminalResizeFromFD(fd int) *nodeoperatorv1.TerminalResize {
	cols, rows, err := term.GetSize(fd)
	if err != nil || cols <= 0 || rows <= 0 {
		return nil
	}
	return &nodeoperatorv1.TerminalResize{Cols: uint32(cols), Rows: uint32(rows)}
}
