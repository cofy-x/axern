package localbundle

import (
	"bytes"
	"strings"
	"testing"
)

func TestEmbeddedBundleIsSelfContainedAndLoopbackOnly(t *testing.T) {
	for _, value := range [][]byte{Compose, CollectorConfig} {
		if len(bytes.TrimSpace(value)) == 0 {
			t.Fatal("embedded local asset is empty")
		}
	}
	if bytes.Contains(Compose, []byte(":latest")) {
		t.Fatal("local bundle contains a floating latest image")
	}
	if !bytes.Contains(Compose, []byte("AXNODED_DNS_NAMESERVERS: ${AXNODED_DNS_NAMESERVERS}")) {
		t.Fatal("local bundle does not pass resolved workload DNS to axnoded")
	}
	for _, port := range []string{"POSTGRES_PORT", "MINIO_API_PORT", "MINIO_CONSOLE_PORT", "CONTROLD_HTTP_PORT", "GATEWAY_CONTROL_PORT", "GATEWAY_HTTP_PORT", "GATEWAY_SSH_PORT", "OTEL_GRPC_PORT", "OTEL_HTTP_PORT", "LGTM_UI_PORT"} {
		mapping := []byte("127.0.0.1:${" + port + "}:")
		if !bytes.Contains(Compose, mapping) {
			t.Fatalf("host port %s is not bound explicitly to loopback", port)
		}
	}
}

func TestReleaseImageLockOverridesEveryImage(t *testing.T) {
	previous := imageLock
	t.Cleanup(func() { imageLock = previous })
	defaults := ImageReferences("1.2.3")
	entries := make([]string, 0, len(defaults))
	for key := range defaults {
		entries = append(entries, key+"=registry.example/"+key+"@sha256:"+strings.Repeat("a", 64))
	}
	imageLock = strings.Join(entries, ";")
	for key, value := range ImageReferences("1.2.3") {
		if !strings.Contains(value, "@sha256:") {
			t.Fatalf("%s is not digest locked: %s", key, value)
		}
	}
}
