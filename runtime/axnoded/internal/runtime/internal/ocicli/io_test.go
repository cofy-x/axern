package ocicli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOutputSnippetUsesTailAndTrimsWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stderr.log")
	data := strings.Repeat("a", 5000) + "  tail-value \n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("write output file: %v", err)
	}

	snippet := ReadOutputSnippet(path)
	if !strings.HasSuffix(snippet, "tail-value") {
		t.Fatalf("snippet = %q, want suffix %q", snippet, "tail-value")
	}
}
