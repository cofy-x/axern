package dnsforward

import (
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestInspectMessageExtractsCanonicalQuestionsAndCNAMEs(t *testing.T) {
	questionName := dnsmessage.MustNewName("BUECHER.example.")
	targetName := dnsmessage.MustNewName("Target.Example.")
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 42, Response: true})
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := builder.Question(dnsmessage.Question{Name: questionName, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatal(err)
	}
	if err := builder.StartAnswers(); err != nil {
		t.Fatal(err)
	}
	if err := builder.CNAMEResource(dnsmessage.ResourceHeader{Name: questionName, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 30}, dnsmessage.CNAMEResource{CNAME: targetName}); err != nil {
		t.Fatal(err)
	}
	wire, err := builder.Finish()
	if err != nil {
		t.Fatal(err)
	}
	message, err := InspectMessage(wire)
	if err != nil {
		t.Fatal(err)
	}
	if message.Header.ID != 42 || len(message.Questions) != 1 || message.Questions[0].Name.String() != "buecher.example." {
		t.Fatalf("unexpected message: %#v", message)
	}
	if len(message.CNAMEs) != 1 || message.CNAMEs[0].Name != "buecher.example" || message.CNAMEs[0].Target != "target.example" {
		t.Fatalf("unexpected CNAMEs: %#v", message.CNAMEs)
	}
}

func TestInspectMessageRejectsMalformedAndOversizedInput(t *testing.T) {
	for _, wire := range [][]byte{nil, {0x00}, make([]byte, MaxDNSMessageBytes+1)} {
		if _, err := InspectMessage(wire); err == nil {
			t.Fatalf("InspectMessage accepted %d bytes", len(wire))
		}
	}
}

func TestInspectMessageAcceptsRootQuestion(t *testing.T) {
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 1})
	if err := builder.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := builder.Question(dnsmessage.Question{Name: dnsmessage.MustNewName("."), Type: dnsmessage.TypeNS, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatal(err)
	}
	wire, err := builder.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectMessage(wire); err != nil {
		t.Fatal(err)
	}
}
