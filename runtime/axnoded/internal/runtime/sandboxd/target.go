package sandboxd

import "strings"

type Target struct {
	SocketPath string
	Client     *Client
}

func TargetForSocket(socketPath string) Target {
	socketPath = strings.TrimSpace(socketPath)
	return Target{SocketPath: socketPath, Client: NewClient(socketPath)}
}
