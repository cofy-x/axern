package l7inspect

import (
	"encoding/binary"
	"testing"
)

func TestParseClientHelloHandlesFragmentedRecordsAndECH(t *testing.T) {
	handshake := clientHelloHandshake("BÜCHER.Example", true)
	records := append(tlsRecord(handshake[:7]), tlsRecord(handshake[7:])...)
	parsed, err := ParseClientHello(records, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ServerName != "xn--bcher-kva.example" || !parsed.HasECH {
		t.Fatalf("unexpected ClientHello: %#v", parsed)
	}
}

func TestParseClientHelloReportsMissingSNI(t *testing.T) {
	parsed, err := ParseClientHello(tlsRecord(clientHelloHandshake("", false)), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ServerName != "" || parsed.HasECH {
		t.Fatalf("unexpected ClientHello: %#v", parsed)
	}
}

func TestParseClientHelloRejectsMalformedAndOversizedInput(t *testing.T) {
	for _, input := range [][]byte{nil, {22, 3, 3, 0, 10, 1}, tlsRecord([]byte{2, 0, 0, 0})} {
		if _, err := ParseClientHello(input, 4096); err == nil {
			t.Fatalf("ParseClientHello accepted malformed input %x", input)
		}
	}
	if _, err := ParseClientHello(make([]byte, 65), 64); err == nil {
		t.Fatal("ParseClientHello accepted an oversized prefix")
	}
}

func clientHelloHandshake(serverName string, ech bool) []byte {
	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0)
	body = append(body, 0, 2, 0x13, 0x01)
	body = append(body, 1, 0)
	var extensions []byte
	if serverName != "" {
		name := []byte(serverName)
		list := make([]byte, 2+1+2+len(name))
		binary.BigEndian.PutUint16(list[:2], uint16(1+2+len(name)))
		binary.BigEndian.PutUint16(list[3:5], uint16(len(name)))
		copy(list[5:], name)
		extensions = appendExtension(extensions, tlsExtensionServerName, list)
	}
	if ech {
		extensions = appendExtension(extensions, tlsExtensionECH, []byte{1, 2, 3})
	}
	body = append(body, byte(len(extensions)>>8), byte(len(extensions)))
	body = append(body, extensions...)
	handshake := []byte{tlsHandshakeClientHello, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	return append(handshake, body...)
}

func appendExtension(out []byte, extensionType uint16, payload []byte) []byte {
	header := make([]byte, 4)
	binary.BigEndian.PutUint16(header[:2], extensionType)
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)))
	out = append(out, header...)
	return append(out, payload...)
}

func tlsRecord(payload []byte) []byte {
	record := []byte{tlsRecordHandshake, 0x03, 0x03, byte(len(payload) >> 8), byte(len(payload))}
	return append(record, payload...)
}
