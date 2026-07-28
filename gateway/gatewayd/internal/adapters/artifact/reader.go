package artifact

import (
	"context"
	"fmt"
	"net/http"
	"time"

	artifactkernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/artifact"
)

type Reader struct{ client *http.Client }

func New(responseHeaderTimeout time.Duration) *Reader {
	transport := &http.Transport{ForceAttemptHTTP2: true, ResponseHeaderTimeout: responseHeaderTimeout}
	return &Reader{client: &http.Client{Transport: transport}}
}
func (r *Reader) Open(ctx context.Context, url string, headers map[string]string) (artifactkernel.Upstream, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return artifactkernel.Upstream{}, fmt.Errorf("build artifact upstream request")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return artifactkernel.Upstream{}, fmt.Errorf("artifact upstream request failed")
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		_ = response.Body.Close()
		return artifactkernel.Upstream{}, fmt.Errorf("artifact upstream returned status %d", response.StatusCode)
	}
	upstream := artifactkernel.Upstream{Body: response.Body, Range: response.StatusCode == http.StatusPartialContent}
	if upstream.Range {
		if _, err := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &upstream.RangeStart, &upstream.RangeEnd, &upstream.RangeSize); err != nil || upstream.RangeStart < 0 || upstream.RangeEnd < upstream.RangeStart || upstream.RangeSize <= upstream.RangeEnd {
			_ = response.Body.Close()
			return artifactkernel.Upstream{}, fmt.Errorf("artifact upstream returned invalid content range")
		}
	}
	return upstream, nil
}

var _ artifactkernel.ByteReader = (*Reader)(nil)
