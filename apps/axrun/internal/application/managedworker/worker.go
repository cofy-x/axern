package managedworker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/durationpb"
)

type Config struct {
	ControlContext   *clientconfig.Context
	ExecutionContext *clientconfig.Context
	BootstrapToken   string
	WorkerID         string
	OutputDir        string
	Concurrency      int
	EstimateCost     func(string, *domain.UsageMetrics) *domain.CostMetrics
}

type Worker struct {
	config Config
	client workerrolloutv1.RolloutWorkerControlClient
}

const workerSessionPrefix = "session-"

func Run(ctx context.Context, config Config) error {
	if config.ControlContext == nil {
		return fmt.Errorf("Axern control context is required")
	}
	if config.ExecutionContext == nil {
		return fmt.Errorf("Axern execution context is required")
	}
	if strings.TrimSpace(config.BootstrapToken) == "" {
		return fmt.Errorf("worker token is required")
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 4
	}
	if config.WorkerID == "" {
		hostname, _ := os.Hostname()
		config.WorkerID = "axrun-" + hostname
	}
	if config.OutputDir == "" {
		config.OutputDir = ".axrun/worker"
	}
	if config.EstimateCost == nil {
		config.EstimateCost = proxy.EstimateCost
	}
	sessionDir, err := prepareOutputSession(config.OutputDir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(sessionDir)
	config.OutputDir = sessionDir
	conn, err := dial(ctx, config.ControlContext)
	if err != nil {
		return err
	}
	defer conn.Close()
	worker := Worker{config: config, client: workerrolloutv1.NewRolloutWorkerControlClient(conn)}
	return worker.run(ctx)
}

func (w Worker) run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	registered, err := w.register(runCtx)
	if err != nil {
		cancel()
		return err
	}
	sem := make(chan struct{}, w.config.Concurrency)
	errCh := make(chan error, w.config.Concurrency)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	for {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case err := <-errCh:
			if err != nil {
				fmt.Fprintf(os.Stderr, "axrun worker: %v\n", err)
			}
		case sem <- struct{}{}:
			response, err := w.client.ClaimWork(runCtx, &workerrolloutv1.ClaimWorkRequest{
				SessionID:    registered.GetSessionID(),
				SessionToken: registered.GetSessionToken(),
				LongPoll:     durationpb.New(20 * time.Second),
			})
			if err != nil {
				<-sem
				if isRetriable(err) {
					if err := waitBackoff(runCtx, time.Second); err != nil {
						return err
					}
					continue
				}
				return err
			}
			if response.GetWork() == nil {
				<-sem
				select {
				case <-runCtx.Done():
					return runCtx.Err()
				case <-time.After(time.Second):
				}
				continue
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-sem }()
				if err := w.execute(runCtx, response.GetWork(), response.GetLeaseToken()); err != nil {
					select {
					case errCh <- err:
					case <-runCtx.Done():
					}
				}
			}()
		}
	}
}

func (w Worker) register(ctx context.Context) (*workerrolloutv1.RegisterWorkerResponse, error) {
	request := &workerrolloutv1.RegisterWorkerRequest{
		WorkerID:  w.config.WorkerID,
		AuthToken: w.config.BootstrapToken,
		Capabilities: &workerrolloutv1.WorkerCapabilities{
			Planner: true,
			Agents:  []string{"command", "codex", "claude-code"},
			WireApis: []agentprofilev1.AgentWireApi{
				agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES,
				agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES,
			},
			MaxConcurrency: int32(w.config.Concurrency),
		},
	}
	backoff := time.Second
	for {
		response, err := w.client.RegisterWorker(ctx, request)
		if err == nil {
			return response, nil
		}
		if !isRetriable(err) {
			return nil, err
		}
		if err := waitBackoff(ctx, backoff); err != nil {
			return nil, err
		}
		backoff = min(10*time.Second, backoff*2)
	}
}

func waitBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// prepareOutputSession gives one worker process an isolated session directory.
// The caller removes it on shutdown; individual work directories are removed
// as soon as their evidence has been committed.
func prepareOutputSession(root string) (string, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create worker output root: %w", err)
	}
	session, err := os.MkdirTemp(root, workerSessionPrefix)
	if err != nil {
		return "", fmt.Errorf("create worker output session: %w", err)
	}
	return session, nil
}

func dial(_ context.Context, config *clientconfig.Context) (*grpc.ClientConn, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.TLS.ServerName}
	pem, err := os.ReadFile(config.TLS.CACert)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("load Axern CA certificate")
	}
	tlsConfig.RootCAs = pool
	certificate, err := tls.LoadX509KeyPair(config.TLS.Cert, config.TLS.Key)
	if err != nil {
		return nil, err
	}
	tlsConfig.Certificates = []tls.Certificate{certificate}
	options := []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))}
	if config.ProxyMode == clientconfig.ProxyModeDirect {
		options = append(options, grpc.WithNoProxy())
	}
	return grpc.NewClient("passthrough:///"+config.Endpoint, options...)
}
