package networkpolicy

import (
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

func FuzzNormalizeDomain(f *testing.F) {
	for _, seed := range []string{
		"example.com", "*.github.com", "BÜCHER.Example.", "https://example.com",
		"127.0.0.1", "*.*.example.com", strings.Repeat("a", 64) + ".example", string([]byte{'0', 0x9c, '0'}),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		normalized, err := NormalizeDomain(raw)
		if err != nil {
			return
		}
		if normalized == "" || len(normalized) > 255 || normalized != strings.ToLower(normalized) || strings.HasSuffix(normalized, ".") {
			t.Fatalf("non-canonical successful result %q", normalized)
		}
		again, err := NormalizeDomain(normalized)
		if err != nil || again != normalized {
			t.Fatalf("normalization is not idempotent: first=%q second=%q err=%v", normalized, again, err)
		}
	})
}

func FuzzNormalizePolicy(f *testing.F) {
	f.Add("Example.COM.", "10.0.0.4/24", uint32(80), uint32(443), uint8(1), false)
	f.Add("*.BÜCHER.example", "169.254.169.254/32", uint32(443), uint32(80), uint8(2), true)
	f.Add("https://example.com", "::/0", uint32(0), uint32(65536), uint8(255), false)
	f.Fuzz(func(t *testing.T, domain, cidr string, start, end uint32, protocol uint8, dnsDeny bool) {
		var spec *commonv1.NetworkSpec
		if dnsDeny {
			spec = dnsDenySpec([]string{domain})
		} else {
			spec = strictSpec([]string{domain}, []*commonv1.CIDREgressRule{{
				Cidr: cidr, Protocol: commonv1.EgressProtocol(protocol),
				Ports: []*commonv1.PortRange{{Start: start, End: end}},
			}})
		}
		normalized, err := Normalize(spec)
		if err != nil {
			return
		}
		again, err := Normalize(normalized)
		if err != nil || !proto.Equal(normalized, again) {
			t.Fatalf("policy normalization is not idempotent: first=%v second=%v err=%v", normalized, again, err)
		}
	})
}
