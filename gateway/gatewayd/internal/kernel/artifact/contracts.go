package artifact

import (
	"context"
	"io"
)

type Resolved struct {
	Size        int64
	Digest, URL string
	Headers     map[string]string
}
type TicketResolver interface {
	ResolveArtifactTicket(context.Context, string, int64) (Resolved, error)
}
type Upstream struct {
	Body       io.ReadCloser
	Range      bool
	RangeStart int64
	RangeEnd   int64
	RangeSize  int64
}
type ByteReader interface {
	Open(context.Context, string, map[string]string) (Upstream, error)
}
