package doctor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc"
)

func TestDiagnoseReadOnlyIsHealthyWithoutMutatingResources(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	tlsConfig := writeTLSFixture(t, now, 90*24*time.Hour)
	environments := &fakeEnvironmentClient{}
	runs := &fakeRunClient{}
	control := New(Options{
		ContextName: "local", Namespace: "default", TLS: tlsConfig, Now: func() time.Time { return now },
		Open: successfulOpener(environments, runs),
	})

	report := control.Diagnose(context.Background())

	if report.Status != StatusHealthy {
		t.Fatalf("status = %q, want healthy: %#v", report.Status, report.Checks)
	}
	if report.Mode != "read_only" {
		t.Fatalf("mode = %q, want read_only", report.Mode)
	}
	if environments.createCalls != 0 || environments.deleteCalls != 0 || runs.createCalls != 0 {
		t.Fatalf("read-only doctor mutated resources: environments=%+v runs=%+v", environments, runs)
	}
	for _, name := range []string{"configuration", "tls_material", "tls_expiry", "tls_key_permissions", "gateway", "identity", "authorization", "namespace", "catalog"} {
		if checkByName(t, report, name).Status != CheckPass {
			t.Fatalf("check %s did not pass: %#v", name, checkByName(t, report, name))
		}
	}
}

func TestDiagnoseReportsExpiringCertificateAsDegraded(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	control := New(Options{
		Namespace: "default", TLS: writeTLSFixture(t, now, 10*24*time.Hour), Now: func() time.Time { return now },
		Open: successfulOpener(&fakeEnvironmentClient{}, &fakeRunClient{}),
	})

	report := control.Diagnose(context.Background())

	if report.Status != StatusDegraded {
		t.Fatalf("status = %q, want degraded", report.Status)
	}
	check := checkByName(t, report, "tls_expiry")
	if check.Status != CheckWarn || check.Code != "tls_certificate_expiring" {
		t.Fatalf("expiry check = %#v", check)
	}
}

func TestDiagnoseGatewayFailureIsSanitizedAndSkipsDependentChecks(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	control := New(Options{
		Namespace: "default", TLS: writeTLSFixture(t, now, 90*24*time.Hour), Now: func() time.Time { return now },
		Open: func(context.Context) (*Session, error) {
			return nil, errors.New("dial sensitive.example:443 using /private/doctor/client.key")
		},
	})

	report := control.Diagnose(context.Background())

	if report.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", report.Status)
	}
	gateway := checkByName(t, report, "gateway")
	if gateway.Code != "gateway_unreachable" || strings.Contains(gateway.Message, "sensitive.example") || strings.Contains(gateway.Message, "/private/doctor") {
		t.Fatalf("gateway failure was not sanitized: %#v", gateway)
	}
	if checkByName(t, report, "namespace").Status != CheckSkip || checkByName(t, report, "catalog").Status != CheckSkip {
		t.Fatalf("gateway dependents were not skipped: %#v", report.Checks)
	}
}

func TestDiagnoseProbeCompletesAndDeletesEnvironment(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	environments := &fakeEnvironmentClient{}
	runs := &fakeRunClient{}
	control := New(Options{
		Namespace: "default", TLS: writeTLSFixture(t, now, 90*24*time.Hour), Now: func() time.Time { return now },
		Probe: &ProbeOptions{TemplateID: "python311", RuntimeClass: "runsc", Timeout: time.Second},
		Open:  successfulOpener(environments, runs),
	})

	report := control.Diagnose(context.Background())

	if report.Status != StatusHealthy {
		t.Fatalf("status = %q, want healthy: %#v", report.Status, report.Checks)
	}
	if checkByName(t, report, "data_plane").Code != "probe_succeeded" {
		t.Fatalf("probe check = %#v", checkByName(t, report, "data_plane"))
	}
	if environments.createCalls != 1 || environments.deleteCalls != 1 || runs.createCalls != 1 {
		t.Fatalf("probe calls: environments=%+v runs=%+v", environments, runs)
	}
	if environments.createRequest.GetSpec().GetTemplateID() != "python311" || environments.createRequest.GetSpec().GetImage() != nil {
		t.Fatalf("probe environment request = %#v", environments.createRequest)
	}
	if runs.createRequest.GetConfig().GetRuntimeClass() != "runsc" || runs.createRequest.GetEnvironmentID() != "env-probe" {
		t.Fatalf("probe run request = %#v", runs.createRequest)
	}
	resources := runs.createRequest.GetConfig().GetResources()
	if resources.GetRequests().GetMemoryBytes() == 0 || resources.GetLimits().GetMemoryBytes() != 0 {
		t.Fatalf("doctor probe memory contract = %#v, want request without hard limit", resources)
	}
}

func TestDiagnoseProbeFailureStillCancelsRunAndDeletesEnvironment(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	environments := &fakeEnvironmentClient{}
	runs := &fakeRunClient{runStatus: runv1.RunStatus_RUN_STATUS_RUNNING}
	control := New(Options{
		Namespace: "default", TLS: writeTLSFixture(t, now, 90*24*time.Hour), Now: func() time.Time { return now },
		Probe: &ProbeOptions{TemplateID: "python311", RuntimeClass: "runsc", Timeout: time.Millisecond},
		Open:  successfulOpener(environments, runs),
	})

	report := control.Diagnose(context.Background())

	if report.Status != StatusFailed || checkByName(t, report, "data_plane").Code != "probe_run_failed" {
		t.Fatalf("probe report = %#v", report)
	}
	if runs.cancelCalls != 1 || environments.deleteCalls != 1 {
		t.Fatalf("failed probe cleanup calls: environments=%+v runs=%+v", environments, runs)
	}
}

func successfulOpener(environments *fakeEnvironmentClient, runs *fakeRunClient) SessionOpener {
	return func(ctx context.Context) (*Session, error) {
		return &Session{
			Context:     ctx,
			Identity:    &fakeIdentityClient{},
			Namespace:   &fakeNamespaceClient{},
			Catalog:     &fakeCatalogClient{},
			Environment: environments,
			Run:         runs,
			Close:       func() error { return nil },
		}, nil
	}
}

type fakeIdentityClient struct{}

func (*fakeIdentityClient) WhoAmI(context.Context, *identityv1.WhoAmIRequest, ...grpc.CallOption) (*identityv1.WhoAmIResponse, error) {
	return &identityv1.WhoAmIResponse{
		Principal:  &identityv1.PrincipalIdentity{PrincipalID: "prn-test", Name: "test"},
		Credential: &identityv1.CredentialIdentity{CredentialID: "cred-test"},
		Roles:      []*identityv1.EffectiveRole{{Role: "namespace_editor", ScopeType: "namespace", Namespace: "default"}},
	}, nil
}

func checkByName(t *testing.T, report Report, name string) Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("report does not contain check %q: %#v", name, report.Checks)
	return Check{}
}

func writeTLSFixture(t *testing.T, now time.Time, validity time.Duration) TLSConfig {
	t.Helper()
	dir := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Axern Test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Axern Doctor"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(validity),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, client, ca, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(dir, "ca.crt")
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return TLSConfig{CACert: caPath, Cert: certPath, Key: keyPath}
}

type fakeNamespaceClient struct{}

func (*fakeNamespaceClient) GetNamespace(context.Context, *namespacev1.GetNamespaceRequest, ...grpc.CallOption) (*namespacev1.GetNamespaceResponse, error) {
	return &namespacev1.GetNamespaceResponse{Namespace: &namespacev1.Namespace{Namespace: "default"}}, nil
}

type fakeCatalogClient struct{}

func (*fakeCatalogClient) ListRuntimeTemplates(context.Context, *catalogv1.ListRuntimeTemplatesRequest, ...grpc.CallOption) (*catalogv1.ListRuntimeTemplatesResponse, error) {
	return &catalogv1.ListRuntimeTemplatesResponse{}, nil
}

type fakeEnvironmentClient struct {
	createCalls   int
	deleteCalls   int
	createRequest *environmentv1.CreateEnvironmentRequest
}

func (f *fakeEnvironmentClient) CreateEnvironment(_ context.Context, request *environmentv1.CreateEnvironmentRequest, _ ...grpc.CallOption) (*environmentv1.CreateEnvironmentResponse, error) {
	f.createCalls++
	f.createRequest = request
	return &environmentv1.CreateEnvironmentResponse{Environment: &environmentv1.Environment{ID: "env-probe"}}, nil
}

func (f *fakeEnvironmentClient) DeleteEnvironment(context.Context, *environmentv1.DeleteEnvironmentRequest, ...grpc.CallOption) (*environmentv1.DeleteEnvironmentResponse, error) {
	f.deleteCalls++
	return &environmentv1.DeleteEnvironmentResponse{Environment: &environmentv1.Environment{ID: "env-probe", Status: environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_DELETED}}, nil
}

type fakeRunClient struct {
	createCalls   int
	cancelCalls   int
	createRequest *runv1.CreateRunRequest
	runStatus     runv1.RunStatus
}

func (f *fakeRunClient) CreateRun(_ context.Context, request *runv1.CreateRunRequest, _ ...grpc.CallOption) (*runv1.CreateRunResponse, error) {
	f.createCalls++
	f.createRequest = request
	return &runv1.CreateRunResponse{Run: &runv1.Run{ID: "run-probe", Status: runv1.RunStatus_RUN_STATUS_RUNNING}}, nil
}

func (f *fakeRunClient) GetRun(context.Context, *runv1.GetRunRequest, ...grpc.CallOption) (*runv1.GetRunResponse, error) {
	status := f.runStatus
	if status == runv1.RunStatus_RUN_STATUS_UNSPECIFIED {
		status = runv1.RunStatus_RUN_STATUS_SUCCEEDED
	}
	return &runv1.GetRunResponse{Run: &runv1.Run{ID: "run-probe", Status: status, ExitCodeKnown: status == runv1.RunStatus_RUN_STATUS_SUCCEEDED}}, nil
}

func (*fakeRunClient) ListRuns(context.Context, *runv1.ListRunsRequest, ...grpc.CallOption) (*runv1.ListRunsResponse, error) {
	return &runv1.ListRunsResponse{}, nil
}

func (f *fakeRunClient) CancelRun(context.Context, *runv1.CancelRunRequest, ...grpc.CallOption) (*runv1.CancelRunResponse, error) {
	f.cancelCalls++
	return &runv1.CancelRunResponse{Run: &runv1.Run{ID: "run-probe", Status: runv1.RunStatus_RUN_STATUS_CANCELLED}}, nil
}
