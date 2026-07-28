package appgateway

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestResolvePortByName(t *testing.T) {
	t.Parallel()
	port, err := ResolvePort([]*commonv1.PortSpec{
		{Name: "http", Protocol: commonv1.PortProtocol_PORT_PROTOCOL_TCP, ContainerPort: 8080},
	}, "http")
	if err != nil {
		t.Fatal(err)
	}
	if port.GetContainerPort() != 8080 || port.GetName() != "http" {
		t.Fatalf("port = %#v", port)
	}
}

func TestResolvePortByNumber(t *testing.T) {
	t.Parallel()
	port, err := ResolvePort([]*commonv1.PortSpec{
		{Name: "http", Protocol: commonv1.PortProtocol_PORT_PROTOCOL_TCP, ContainerPort: 8080},
	}, "8080")
	if err != nil {
		t.Fatal(err)
	}
	if port.GetContainerPort() != 8080 {
		t.Fatalf("container port = %d, want 8080", port.GetContainerPort())
	}
}

func TestResolvePortByNumberWithoutPortSpec(t *testing.T) {
	t.Parallel()
	port, err := ResolvePort(nil, "8080")
	if err != nil {
		t.Fatal(err)
	}
	if port.GetContainerPort() != 8080 || port.GetProtocol() != commonv1.PortProtocol_PORT_PROTOCOL_TCP {
		t.Fatalf("port = %#v", port)
	}
}

func TestResolvePortMissing(t *testing.T) {
	t.Parallel()
	if _, err := ResolvePort([]*commonv1.PortSpec{{Name: "http", ContainerPort: 8080}}, "metrics"); err == nil {
		t.Fatal("ResolvePort() unexpectedly succeeded")
	}
}
