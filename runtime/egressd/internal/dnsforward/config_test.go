package dnsforward

import "testing"

func TestParseUpstreamsRequiresExplicitIPResolvers(t *testing.T) {
	if _, err := ParseUpstreams(nil); err == nil {
		t.Fatal("ParseUpstreams accepted an empty resolver set")
	}
	got, err := ParseUpstreams([]string{"10.0.0.2", "[2001:db8::53]:5353", "10.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].String() != "10.0.0.2:53" || got[1].String() != "[2001:db8::53]:5353" {
		t.Fatalf("unexpected upstreams: %v", got)
	}
	for _, invalid := range []string{
		"dns.example:53", "10.0.0.2:0", "", "2001:db8::zz",
		"127.0.0.1", "[::1]:53", "0.0.0.0", "::", "224.0.0.1",
	} {
		if _, err := ParseUpstreams([]string{invalid}); err == nil {
			t.Fatalf("ParseUpstreams accepted %q", invalid)
		}
	}
}
