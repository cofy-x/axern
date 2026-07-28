package output

import (
	"bytes"
	"testing"
)

func TestRestoreTerminalShowsCursor(t *testing.T) {
	var out bytes.Buffer
	RestoreTerminal(&out)
	if got := out.String(); got != "\x1b[0m\x1b[?25h" {
		t.Fatalf("RestoreTerminal wrote %q, want reset plus show cursor", got)
	}
}
