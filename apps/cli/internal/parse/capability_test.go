package parse

import "testing"

func TestExtensionCapabilitiesParsesExactValues(t *testing.T) {
	requirements, err := ExtensionCapabilities([]string{"example.com/accelerator=v1", "example.net/feature", "example.org/exact= value "})
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 3 || requirements[0].GetCapability().GetName() != "example.com/accelerator" || requirements[0].GetCapability().GetValue() != "v1" || requirements[1].GetCapability().GetValue() != "" || requirements[2].GetCapability().GetValue() != " value " {
		t.Fatalf("requirements = %#v", requirements)
	}
	if _, err := ExtensionCapabilities([]string{"=value"}); err == nil {
		t.Fatal("empty extension capability name was accepted")
	}
	if _, err := ExtensionCapabilities([]string{"example.com/accelerator=v1", "example.com/accelerator=v2"}); err == nil {
		t.Fatal("multiple exact values for one extension capability name were accepted")
	}
	if _, err := ExtensionCapabilities([]string{"axern.io/internal=true"}); err == nil {
		t.Fatal("reserved extension capability domain was accepted")
	}
	if _, err := ExtensionCapabilities([]string{" example.com/accelerator=v1"}); err == nil {
		t.Fatal("extension capability name with surrounding whitespace was accepted")
	}
}
