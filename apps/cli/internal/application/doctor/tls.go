package doctor

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"runtime"
	"time"
)

const certificateWarningWindow = 30 * 24 * time.Hour

func inspectTLS(config TLSConfig, now time.Time) []Check {
	started := time.Now()
	pair, err := tls.LoadX509KeyPair(config.Cert, config.Key)
	if err != nil || len(pair.Certificate) == 0 {
		return failedTLSChecks("tls_material_invalid", "mTLS material could not be loaded", "replace the client certificate and key, then import the context again", started)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return failedTLSChecks("tls_certificate_invalid", "the client certificate could not be parsed", "replace the client certificate, then import the context again", started)
	}
	caPEM, err := os.ReadFile(config.CACert)
	if err != nil {
		return failedTLSChecks("tls_ca_unavailable", "the gateway CA certificate could not be loaded", "restore the CA certificate or import the context again", started)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return failedTLSChecks("tls_ca_invalid", "the gateway CA certificate could not be parsed", "replace the CA certificate or import the context again", started)
	}

	checks := []Check{passedCheck("tls_material", "tls_material_valid", "mTLS certificate, key, and CA material are readable", started)}
	expiryStarted := time.Now()
	switch {
	case now.Before(leaf.NotBefore):
		checks = append(checks, failedCheck("tls_expiry", "tls_certificate_not_yet_valid", "the client certificate is not valid yet", "check system time and certificate issuance", expiryStarted))
	case !now.Before(leaf.NotAfter):
		checks = append(checks, failedCheck("tls_expiry", "tls_certificate_expired", "the client certificate has expired", "rotate the client certificate and import the context again", expiryStarted))
	case leaf.NotAfter.Sub(now) <= certificateWarningWindow:
		remainingDays := int(leaf.NotAfter.Sub(now).Hours() / 24)
		checks = append(checks, Check{
			Name: "tls_expiry", Status: CheckWarn, Code: "tls_certificate_expiring",
			Message:     fmt.Sprintf("client certificate expires in %d days", remainingDays),
			Remediation: "rotate the client certificate before it expires", DurationMS: elapsedMilliseconds(expiryStarted),
		})
	default:
		checks = append(checks, passedCheck("tls_expiry", "tls_certificate_current", "client certificate validity window is healthy", expiryStarted))
	}

	permissionsStarted := time.Now()
	if runtime.GOOS == "windows" {
		checks = append(checks, Check{Name: "tls_key_permissions", Status: CheckSkip, Code: "not_applicable", Message: "private key mode is not evaluated on Windows"})
		return checks
	}
	info, err := os.Stat(config.Key)
	if err != nil {
		checks = append(checks, failedCheck("tls_key_permissions", "tls_key_unavailable", "client private key metadata could not be read", "restore the client private key or import the context again", permissionsStarted))
	} else if info.Mode().Perm()&0o077 != 0 {
		checks = append(checks, Check{
			Name: "tls_key_permissions", Status: CheckWarn, Code: "tls_key_permissions",
			Message: "client private key permissions are too broad", Remediation: "restrict the client private key to its owner",
			DurationMS: elapsedMilliseconds(permissionsStarted),
		})
	} else {
		checks = append(checks, passedCheck("tls_key_permissions", "tls_key_permissions_restricted", "client private key permissions are restricted", permissionsStarted))
	}
	return checks
}

func failedTLSChecks(code, message, remediation string, started time.Time) []Check {
	return []Check{
		failedCheck("tls_material", code, message, remediation, started),
		skippedCheck("tls_expiry", "mTLS material validation did not pass"),
		skippedCheck("tls_key_permissions", "mTLS material validation did not pass"),
	}
}

func elapsedMilliseconds(started time.Time) int64 {
	duration := time.Since(started)
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

func passedCheck(name, code, message string, started time.Time) Check {
	return Check{Name: name, Status: CheckPass, Code: code, Message: message, DurationMS: elapsedMilliseconds(started)}
}

func failedCheck(name, code, message, remediation string, started time.Time) Check {
	return Check{Name: name, Status: CheckFail, Code: code, Message: message, Remediation: remediation, DurationMS: elapsedMilliseconds(started)}
}

func skippedCheck(name, message string) Check {
	return Check{Name: name, Status: CheckSkip, Code: "dependency_failed", Message: message}
}
