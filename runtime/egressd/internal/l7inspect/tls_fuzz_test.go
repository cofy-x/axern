package l7inspect

import (
	"testing"

	"github.com/cofy-x/axern/lib/go/networkpolicy"
)

func FuzzParseClientHello(f *testing.F) {
	for _, seed := range [][]byte{
		tlsRecord(clientHelloHandshake("example.com", false)),
		append(tlsRecord(clientHelloHandshake("example.com", true)[:7]), tlsRecord(clientHelloHandshake("example.com", true)[7:])...),
		{22, 3, 3, 0, 10, 1},
		{23, 3, 3, 0, 0},
		append([]byte{22, '0', '0', 0, 67, 1, 0, 0, 34}, []byte("000000000000000000000000000000000000000000000000000000000000000")...),
	} {
		f.Add(seed, uint16(4096))
	}
	f.Fuzz(func(t *testing.T, records []byte, bound uint16) {
		maxBytes := int(bound)
		if maxBytes == 0 {
			maxBytes = 1
		}
		hello, err := ParseClientHello(records, maxBytes)
		if err != nil {
			return
		}
		if len(records) > maxBytes {
			t.Fatalf("oversized ClientHello was accepted: bytes=%d max=%d", len(records), maxBytes)
		}
		if hello.ServerName != "" {
			normalized, err := networkpolicy.NormalizeDomain(hello.ServerName)
			if err != nil || normalized != hello.ServerName {
				t.Fatalf("successful SNI is not canonical: name=%q normalized=%q err=%v", hello.ServerName, normalized, err)
			}
		}
		again, err := ParseClientHello(records, maxBytes)
		if err != nil || again != hello {
			t.Fatalf("TLS inspection is not deterministic: first=%#v second=%#v err=%v", hello, again, err)
		}
	})
}
