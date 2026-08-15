package image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/urfave/cli"
)

const nodeAllInOneImagemgrSocket = "/run/imagemgr/imagemgr.sock"

func imagemgrSocketFlag() cli.StringFlag {
	return cli.StringFlag{
		Name:   "imagemgr-socket",
		Usage:  "local unix socket path for imagemgr",
		Value:  config.DefaultImageManagerSocket,
		EnvVar: "IMAGEMGR_SOCKET",
	}
}

func newImagemgrHTTPClient(socketPath string, timeout time.Duration) (*http.Client, error) {
	socketPath = resolveImagemgrSocketPath(socketPath)
	if socketPath == "" {
		return nil, fmt.Errorf("imagemgr socket path is required")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{Timeout: timeout}
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}, nil
}

func resolveImagemgrSocketPath(socketPath string) string {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == config.DefaultImageManagerSocket {
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			if _, fallbackErr := os.Stat(nodeAllInOneImagemgrSocket); fallbackErr == nil {
				return nodeAllInOneImagemgrSocket
			}
		}
	}
	return socketPath
}

func fetchInventory(socketPath string, timeout time.Duration) (*inventoryResponse, error) {
	client, err := newImagemgrHTTPClient(socketPath, timeout)
	if err != nil {
		return nil, err
	}
	resp, err := client.Get("http://unix/inventory")
	if err != nil {
		return nil, fmt.Errorf("fetch imagemgr inventory: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch imagemgr inventory failed: %s", strings.TrimSpace(string(errBody)))
	}
	var inventory inventoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&inventory); err != nil {
		return nil, fmt.Errorf("decode imagemgr inventory response: %w", err)
	}
	return &inventory, nil
}

func postImport(socketPath string, req importRequest, timeout time.Duration) (*importResponse, error) {
	client, err := newImagemgrHTTPClient(socketPath, timeout)
	if err != nil {
		return nil, err
	}
	// The global axctl timeout remains the UDS connect timeout, but importing a
	// large archive must not inherit its short whole-request deadline. The
	// producer pipe and process context own cancellation for this stream.
	client.Timeout = 0
	endpoint := "http://unix/oci_import?ref=" + url.QueryEscape(req.ImageRef)
	httpResp, err := client.Post(endpoint, "application/x-tar", req.Archive)
	if err != nil {
		return nil, fmt.Errorf("import image %s: %w", req.ImageRef, err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("import image %s failed: %s", req.ImageRef, strings.TrimSpace(string(errBody)))
	}
	var resp importResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode imagemgr import response: %w", err)
	}
	if resp.CanonicalRef == "" || resp.ImmutableRef == "" || resp.GenerationDigest == "" {
		return nil, fmt.Errorf("imagemgr import response missing canonical_ref, immutable_ref, or generation_digest")
	}
	return &resp, nil
}
