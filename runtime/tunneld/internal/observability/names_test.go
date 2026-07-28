package observability

import (
	"strings"
	"testing"
)

func TestTunnelMetricLabelsStayLowCardinality(t *testing.T) {
	labels := []string{AttrPeerKind, AttrFrameKind, AttrRelayID, AttrCloseReason}
	for _, label := range labels {
		for _, forbidden := range []string{"session", "allocation", "token"} {
			if strings.Contains(label, forbidden) {
				t.Fatalf("tunnel metric label %q contains forbidden high-cardinality term %q", label, forbidden)
			}
		}
	}
}
