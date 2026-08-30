package dnsforward

import (
	"reflect"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func FuzzInspectMessage(f *testing.F) {
	valid, err := (&dnsmessage.Message{
		Header: dnsmessage.Header{ID: 7, RecursionDesired: true},
		Questions: []dnsmessage.Question{{
			Name: dnsmessage.MustNewName("Example.COM."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET,
		}},
	}).Pack()
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{valid, nil, {0}, make([]byte, 12), append([]byte(nil), valid[:len(valid)-1]...)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, wire []byte) {
		message, err := InspectMessage(wire)
		if err != nil {
			return
		}
		again, err := InspectMessage(wire)
		if err != nil || !reflect.DeepEqual(message, again) {
			t.Fatalf("DNS inspection is not deterministic: first=%#v second=%#v err=%v", message, again, err)
		}
		for _, question := range message.Questions {
			name := question.Name.String()
			if name != "." && name[len(name)-1] != '.' {
				t.Fatalf("question name is not canonical: %q", name)
			}
		}
	})
}
