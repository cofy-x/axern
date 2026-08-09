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
}
