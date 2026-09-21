package hostlinux

import "path/filepath"

// RunscSentryBinary returns the sentry component installed beside the locked
// runsc release. Modern gVisor releases execute the OCI state PID from this
// component rather than from the runsc launcher itself.
func RunscSentryBinary(runtimeBinary string) string {
	return filepath.Join(filepath.Dir(runtimeBinary), "gvisor-bin", "gvisor_sentry")
}
