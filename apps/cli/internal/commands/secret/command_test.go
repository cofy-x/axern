package secret

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
)

func TestReadSecretLiterals(t *testing.T) {
	values, err := readSecretLiterals(strings.NewReader("API_KEY=secret\r\nEMPTY=\r\nPADDED=  preserve me  \r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["API_KEY"] != "secret" || values["EMPTY"] != "" || values["PADDED"] != "  preserve me  " {
		t.Fatalf("values = %#v", values)
	}
}

func TestSecretCreateOnlyExposesStdinForOpaqueValues(t *testing.T) {
	cmd := createCommand(command.Runtime{})
	if cmd.Flags().Lookup("literal-stdin") == nil {
		t.Fatal("secret create is missing --literal-stdin")
	}
	if cmd.Flags().Lookup("literal") != nil {
		t.Fatal("secret create must not accept opaque values in argv")
	}
}

func TestReadSecretLiteralsRejectsMalformedInput(t *testing.T) {
	if _, err := readSecretLiterals(strings.NewReader("not-a-key-value\n")); err == nil {
		t.Fatal("readSecretLiterals accepted malformed input")
	}
}
