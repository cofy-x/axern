package sandboxd

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

type Client struct {
	socketPath string
	httpClient *http.Client
}

const maxUnixSocketPathLen = 100

func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			dialPath, cleanup, err := shortUnixSocketPath(socketPath)
			if err != nil {
				return nil, err
			}
			defer cleanup()
			return dialer.DialContext(ctx, "unix", dialPath)
		},
	}
	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{Transport: transport},
	}
}

func shortUnixSocketPath(socketPath string) (string, func(), error) {
	if len(socketPath) <= maxUnixSocketPathLen {
		return socketPath, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "axsd-*")
	if err != nil {
		return "", func() {}, err
	}
	dialPath := filepath.Join(dir, "s.sock")
	if err := os.Symlink(socketPath, dialPath); err != nil {
		_ = os.RemoveAll(dir)
		return "", func() {}, err
	}
	return dialPath, func() { _ = os.RemoveAll(dir) }, nil
}
