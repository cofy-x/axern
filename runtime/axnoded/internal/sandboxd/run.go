package sandboxd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
	daemonprocess "github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/process"
	daemonserver "github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/server"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
)

func RunCLI(args []string, stdout io.Writer, stderr io.Writer) int {
	cfg, err := ParseConfig(args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "axern-sandboxd: %v\n", err)
		return 2
	}
	runner := NewRunner(cfg, stdout, stderr)
	code, err := runner.Run(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "axern-sandboxd: %v\n", err)
		if code == 0 {
			return 1
		}
	}
	return code
}

type Runner struct {
	cfg    Config
	stdout io.Writer
	stderr io.Writer
	state  *workload.State
	waiter *proc.Waiter
}

func NewRunner(cfg Config, stdout io.Writer, stderr io.Writer) *Runner {
	return &Runner{
		cfg:    cfg,
		stdout: stdout,
		stderr: stderr,
		state:  workload.NewState(cfg.SocketPath),
	}
}

func (r *Runner) Run(ctx context.Context) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r.waiter = proc.NewWaiter(ctx)
	defer r.waiter.Stop()

	processes := daemonprocess.NewRegistry(r.waiter, r.cfg.Entrypoint.Env, r.cfg.Entrypoint.Cwd)
	server := daemonserver.New(r.state, processes, r.waiter)
	httpServer := &http.Server{Handler: server}
	listener, err := listenUnix(r.cfg.SocketPath)
	if err != nil {
		return 1, err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(r.cfg.SocketPath)
	}()

	serverErr := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	supervisor := workload.NewSupervisor(r.cfg.Entrypoint, r.cfg.ShutdownTimeout, r.state, r.waiter, r.stdout, r.stderr)
	userDone := supervisor.Start()
	signalCh := make(chan os.Signal, 2)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	var code int
	select {
	case result := <-userDone:
		code = result.ExitCode
	case sig := <-signalCh:
		result := supervisor.Shutdown(sig)
		code = result.ExitCode
	case err := <-serverErr:
		if err != nil {
			cancel()
			return 1, err
		}
	case <-ctx.Done():
		result := supervisor.Shutdown(syscall.SIGTERM)
		code = result.ExitCode
	}

	httpShutdownCtx, httpShutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer httpShutdownCancel()
	_ = httpServer.Shutdown(httpShutdownCtx)
	providerShutdownCtx, providerShutdownCancel := context.WithTimeout(context.Background(), r.cfg.ShutdownTimeout+time.Second)
	defer providerShutdownCancel()
	_ = server.Shutdown(providerShutdownCtx, r.cfg.ShutdownTimeout)
	cancel()
	if err := <-serverErr; err != nil {
		return code, err
	}
	return code, nil
}

func listenUnix(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return nil, err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}
