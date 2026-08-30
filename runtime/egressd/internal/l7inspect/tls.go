package l7inspect

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"

	"github.com/cofy-x/axern/lib/go/networkpolicy"
)

const DefaultMaxClientHelloBytes = 64 * 1024

const (
	tlsRecordHandshake      = 22
	tlsHandshakeClientHello = 1
	tlsExtensionServerName  = 0
	tlsExtensionECH         = 0xfe0d
	tlsExtensionECHDraft    = 0xffce
)

type ClientHello struct {
	ServerName string
	HasECH     bool
}

// ParseClientHello reassembles a ClientHello split across TLS records and
// inspects only the bounded cleartext handshake prefix.
func ParseClientHello(records []byte, maxBytes int) (ClientHello, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxClientHelloBytes
	}
	if len(records) == 0 || len(records) > maxBytes {
		return ClientHello{}, fmt.Errorf("TLS prefix size must be 1..%d bytes", maxBytes)
	}
	var handshake []byte
	for offset := 0; offset < len(records); {
		if len(records)-offset < 5 {
			return ClientHello{}, fmt.Errorf("truncated TLS record header")
		}
		if records[offset] != tlsRecordHandshake {
			return ClientHello{}, fmt.Errorf("expected TLS handshake record")
		}
		length := int(binary.BigEndian.Uint16(records[offset+3 : offset+5]))
		offset += 5
		if length > len(records)-offset {
			return ClientHello{}, fmt.Errorf("truncated TLS record payload")
		}
		if len(handshake)+length > maxBytes {
			return ClientHello{}, fmt.Errorf("TLS ClientHello exceeds %d bytes", maxBytes)
		}
		handshake = append(handshake, records[offset:offset+length]...)
		offset += length
		if len(handshake) >= 4 {
			handshakeLength := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
			if handshakeLength+4 > maxBytes {
				return ClientHello{}, fmt.Errorf("TLS ClientHello exceeds %d bytes", maxBytes)
			}
			if len(handshake) >= handshakeLength+4 {
				return inspectClientHello(handshake[:handshakeLength+4])
			}
		}
	}
	return ClientHello{}, fmt.Errorf("incomplete TLS ClientHello")
}

func inspectClientHello(handshake []byte) (ClientHello, error) {
	if len(handshake) < 4 || handshake[0] != tlsHandshakeClientHello {
		return ClientHello{}, fmt.Errorf("expected TLS ClientHello")
	}
	body := handshake[4:]
	if len(body) < 35 {
		return ClientHello{}, fmt.Errorf("truncated TLS ClientHello fixed fields")
	}
	offset := 34
	if int(body[offset]) > 32 {
		return ClientHello{}, fmt.Errorf("TLS session id exceeds 32 bytes")
	}
	var err error
	offset, err = skipVector8(body, offset, "session id")
	if err != nil {
		return ClientHello{}, err
	}
	if len(body)-offset < 2 {
		return ClientHello{}, fmt.Errorf("truncated TLS cipher suites")
	}
	cipherSuitesLength := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	if cipherSuitesLength < 2 || cipherSuitesLength%2 != 0 {
		return ClientHello{}, fmt.Errorf("TLS cipher suites must contain complete 2-byte entries")
	}
	offset, err = skipVector16(body, offset, "cipher suites")
	if err != nil {
		return ClientHello{}, err
	}
	if offset >= len(body) || body[offset] == 0 {
		return ClientHello{}, fmt.Errorf("TLS compression methods must not be empty")
	}
	offset, err = skipVector8(body, offset, "compression methods")
	if err != nil {
		return ClientHello{}, err
	}
	if offset == len(body) {
		return ClientHello{}, nil
	}
	if len(body)-offset < 2 {
		return ClientHello{}, fmt.Errorf("truncated TLS extension block")
	}
	extensionsLength := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	offset += 2
	if extensionsLength != len(body)-offset {
		return ClientHello{}, fmt.Errorf("invalid TLS extension block length")
	}
	result := ClientHello{}
	seenExtensions := make(map[uint16]struct{})
	end := offset + extensionsLength
	for offset < end {
		if end-offset < 4 {
			return ClientHello{}, fmt.Errorf("truncated TLS extension header")
		}
		extensionType := binary.BigEndian.Uint16(body[offset : offset+2])
		extensionLength := int(binary.BigEndian.Uint16(body[offset+2 : offset+4]))
		offset += 4
		if _, duplicate := seenExtensions[extensionType]; duplicate {
			return ClientHello{}, fmt.Errorf("duplicate TLS extension 0x%04x", extensionType)
		}
		seenExtensions[extensionType] = struct{}{}
		if extensionLength > end-offset {
			return ClientHello{}, fmt.Errorf("truncated TLS extension payload")
		}
		payload := body[offset : offset+extensionLength]
		offset += extensionLength
		switch extensionType {
		case tlsExtensionECH, tlsExtensionECHDraft:
			result.HasECH = true
		case tlsExtensionServerName:
			serverName, err := parseServerName(payload)
			if err != nil {
				return ClientHello{}, err
			}
			if result.ServerName != "" && serverName != "" {
				return ClientHello{}, fmt.Errorf("duplicate TLS server name extension")
			}
			result.ServerName = serverName
		}
	}
	return result, nil
}

func parseServerName(payload []byte) (string, error) {
	if len(payload) < 2 {
		return "", fmt.Errorf("truncated TLS server name list")
	}
	length := int(binary.BigEndian.Uint16(payload[:2]))
	if length != len(payload)-2 {
		return "", fmt.Errorf("invalid TLS server name list length")
	}
	serverName := ""
	for offset := 2; offset < len(payload); {
		if len(payload)-offset < 3 {
			return "", fmt.Errorf("truncated TLS server name")
		}
		nameType := payload[offset]
		nameLength := int(binary.BigEndian.Uint16(payload[offset+1 : offset+3]))
		offset += 3
		if nameLength > len(payload)-offset {
			return "", fmt.Errorf("truncated TLS server name")
		}
		name := string(payload[offset : offset+nameLength])
		offset += nameLength
		if nameType != 0 {
			continue
		}
		if serverName != "" {
			return "", fmt.Errorf("duplicate TLS host_name entry")
		}
		if _, err := netip.ParseAddr(name); err == nil {
			return "", fmt.Errorf("TLS SNI must not be an IP literal")
		}
		normalized, err := networkpolicy.NormalizeDomain(name)
		if err != nil || strings.HasPrefix(normalized, "*.") {
			return "", fmt.Errorf("invalid TLS SNI")
		}
		serverName = normalized
	}
	return serverName, nil
}

func skipVector8(data []byte, offset int, name string) (int, error) {
	if offset >= len(data) {
		return 0, fmt.Errorf("truncated TLS %s", name)
	}
	length := int(data[offset])
	offset++
	if length > len(data)-offset {
		return 0, fmt.Errorf("truncated TLS %s", name)
	}
	return offset + length, nil
}

func skipVector16(data []byte, offset int, name string) (int, error) {
	if len(data)-offset < 2 {
		return 0, fmt.Errorf("truncated TLS %s", name)
	}
	length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if length > len(data)-offset {
		return 0, fmt.Errorf("truncated TLS %s", name)
	}
	return offset + length, nil
}
