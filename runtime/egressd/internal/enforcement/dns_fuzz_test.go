package enforcement

import (
	"testing"

	"github.com/cofy-x/axern/runtime/egressd/internal/dnsforward"
	"golang.org/x/net/dns/dnsmessage"
)

func FuzzStrictDNSAuthorizations(f *testing.F) {
	f.Add([]byte{0, 7, 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1})
	f.Add([]byte{0, 7, 0x81, 0x80, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, wire []byte) {
		if len(wire) == 0 || len(wire) > dnsforward.MaxDNSMessageBytes {
			return
		}
		var message dnsmessage.Message
		if err := message.Unpack(wire); err != nil {
			return
		}
		_, _ = strictDNSAuthorizations(message.Questions, message.Answers)
	})
}
