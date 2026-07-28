package function

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	appfunction "github.com/cofy-x/axern/apps/cli/internal/application/function"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/controlv1"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/resourcespec"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/spf13/cobra"
)

var zeroTime = time.Unix(0, 0)

func deployCommand(runtime command.Runtime) *cobra.Command {
	var file string
	var wait bool
	var waitTimeout time.Duration
	cmd := &cobra.Command{
		Use: "deploy", Short: "Deploy a function from an Axern resource spec", Args: command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return command.Usage(fmt.Errorf("--file is required"))
			}
			envelope, err := resourcespec.Load(file, resourcespec.KindFunction)
			if err != nil {
				return command.Usage(err)
			}
			sourcePath, err := envelope.FunctionSourcePath()
			if err != nil {
				return command.Usage(err)
			}
			bundlePayload, bundleDigest, err := packageBundle(sourcePath)
			if err != nil {
				return command.Usage(err)
			}

			session, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer session.Close()
			functions := appfunction.New(session.Clients.Function)

			uploadResp, err := uploadBundleBytes(session, envelope.Metadata.Namespace, envelope.Metadata.Name, bundlePayload, bundleDigest)
			if err != nil {
				return err
			}

			execution, err := envelope.ExecutionConfig()
			if err != nil {
				return command.Usage(err)
			}
			functionSpec := envelope.Spec.Function
			environmentID, environment := envelope.EnvironmentSpec()
			deployParams := appfunction.DeployParams{
				Namespace:      envelope.Metadata.Namespace,
				Name:           envelope.Metadata.Name,
				Runtime:        functionSpec.Runtime,
				Handler:        functionSpec.Handler,
				Initializer:    functionSpec.Initializer,
				Labels:         envelope.Metadata.Labels,
				TimeoutSeconds: functionSpec.TimeoutSeconds,
				Env:            execution.Env,
				Resources:      execution.Resources,
				VolumeMounts:   execution.VolumeMounts,
				EnvironmentID:  environmentID,
				Environment:    environment,
				BundleURI:      uploadResp.GetBundle().GetStorageUri(),
				BundleDigest:   uploadResp.GetBundle().GetDigest(),
				BundleSize:     uploadResp.GetBundle().GetSizeBytes(),
			}
			if scaling := functionSpec.Scaling; scaling.MinReplicas != 0 || scaling.MaxReplicas != 0 || scaling.Concurrency != 0 || scaling.IdleTimeout != "" {
				idleTimeout, err := time.ParseDuration(scaling.IdleTimeout)
				if err != nil {
					return command.Usage(err)
				}
				deployParams.Scaling = &appfunction.ScalingParams{
					MinReplicas: int(scaling.MinReplicas),
					MaxReplicas: int(scaling.MaxReplicas),
					Concurrency: int(scaling.Concurrency),
					IdleSeconds: int(idleTimeout.Seconds()),
				}
			}

			resp, err := functions.Deploy(session.Context, deployParams)
			if err != nil {
				return err
			}

			if wait {
				fmt.Fprintf(cmd.ErrOrStderr(), "Function deployed: %s (waiting for ready)\n", resp.GetFunction().GetID())
				getResp, waitErr := waitFunctionReady(session, functions, resp.GetFunction().GetID(), waitTimeout, cmd.ErrOrStderr())
				if getResp != nil {
					if runtime.Options.Output == "json" {
						return output.PrintFunctionGetResponseJSON(cmd.OutOrStdout(), getResp)
					}
					output.RenderFunction(cmd.OutOrStdout(), getResp.GetFunction(), getResp.GetDeployment())
				}
				return waitErr
			}

			if runtime.Options.Output == "json" {
				return output.PrintFunctionDeployResponseJSON(cmd.OutOrStdout(), resp)
			}
			output.RenderFunction(cmd.OutOrStdout(), resp.GetFunction(), resp.GetDeployment())
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "path to an axern/v1 Function spec")
	cmd.Flags().BoolVar(&wait, "wait", false, "wait for readiness")
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 3*time.Minute, "readiness timeout; 0 disables it")
	return cmd
}

func packageBundle(sourcePath string) ([]byte, string, error) {
	type entry struct {
		rel  string
		data []byte
	}
	var entries []entry

	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink: %s", path)
		}
		rel, relErr := filepath.Rel(sourcePath, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		entries = append(entries, entry{rel: filepath.ToSlash(filepath.Join("src", rel)), data: data})
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("walk source: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].rel < entries[j].rel
	})

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:    e.rel,
			Size:    int64(len(e.data)),
			Mode:    0644,
			ModTime: zeroTime,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, "", err
		}
		if _, err := tw.Write(e.data); err != nil {
			return nil, "", err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}

	payload := buf.Bytes()
	h := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(h[:])
	return payload, digest, nil
}

func uploadBundleBytes(
	session *controlv1.Session,
	namespace, name string,
	payload []byte,
	digest string,
) (*functionv1.UploadFunctionBundleResponse, error) {
	stream, err := session.Clients.Function.UploadFunctionBundle(session.Context)
	if err != nil {
		return nil, fmt.Errorf("open upload stream: %w", err)
	}
	if err := stream.Send(&functionv1.UploadFunctionBundleRequest{
		Request: &functionv1.UploadFunctionBundleRequest_Open{
			Open: &functionv1.UploadFunctionBundleOpen{
				Namespace: namespace,
				Name:      name,
				Digest:    digest,
				MediaType: "application/vnd.axern.function.tar",
				SizeBytes: int64(len(payload)),
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("send open: %w", err)
	}
	const chunkSize = 1024 * 1024
	for offset := 0; offset < len(payload); offset += chunkSize {
		end := offset + chunkSize
		if end > len(payload) {
			end = len(payload)
		}
		if err := stream.Send(&functionv1.UploadFunctionBundleRequest{
			Request: &functionv1.UploadFunctionBundleRequest_Chunk{Chunk: payload[offset:end]},
		}); err != nil {
			return nil, fmt.Errorf("send chunk: %w", err)
		}
	}
	return stream.CloseAndRecv()
}

func waitFunctionReady(
	session *controlv1.Session,
	functions appfunction.Control,
	functionID string,
	timeout time.Duration,
	w io.Writer,
) (*functionv1.GetFunctionResponse, error) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	var last *functionv1.GetFunctionResponse
	for deadline.IsZero() || time.Now().Before(deadline) {
		resp, err := functions.Get(session.Context, functionID, "", "")
		if err != nil {
			return last, err
		}
		last = resp
		if functionReady(resp) {
			return resp, nil
		}
		fn := resp.GetFunction()
		dep := resp.GetDeployment()
		if fn.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_FAILED ||
			dep.GetStatus() == functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_FAILED {
			return resp, fmt.Errorf("function deploy failed: %s", fn.GetMessage())
		}
		status := output.FunctionDeploymentStatusLabel(dep.GetStatus())
		fmt.Fprintf(w, "waiting: deployment=%s ready=%d/%d\n", status, dep.GetReadyReplicas(), dep.GetDesiredReplicas())
		time.Sleep(2 * time.Second)
	}
	return last, fmt.Errorf("function did not become ready within %s", timeout)
}

func functionReady(resp *functionv1.GetFunctionResponse) bool {
	fn := resp.GetFunction()
	dep := resp.GetDeployment()
	if fn.GetStatus() != functionv1.FunctionStatus_FUNCTION_STATUS_READY {
		return false
	}
	switch dep.GetStatus() {
	case functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY:
		return dep.GetDesiredReplicas() > 0 && dep.GetReadyReplicas() >= dep.GetDesiredReplicas()
	case functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO:
		return dep.GetDesiredReplicas() == 0 && dep.GetReadyReplicas() == 0
	default:
		return false
	}
}
