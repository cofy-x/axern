package controlv1

import (
	"context"
	"time"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

const (
	DefaultTarget    = "127.0.0.1:25000"
	DefaultTLSCACert = ".dev/certs/ca.crt"
	DefaultTLSCert   = ".dev/certs/client.crt"
	DefaultTLSKey    = ".dev/certs/client.key"
	DefaultProxyMode = ProxyModeEnv
	ProxyModeEnv     = "env"
	ProxyModeDirect  = "direct"
)

type Config struct {
	Endpoint      string
	TLSCACert     string
	TLSCert       string
	TLSKey        string
	TLSServerName string
	ProxyMode     string
	Timeout       time.Duration
}

type SessionOpener func(context.Context) (*Session, error)

func Opener(config Config) SessionOpener {
	return func(ctx context.Context) (*Session, error) {
		return Open(ctx, config)
	}
}

type Clients struct {
	Admin            adminv1.AllocationLifecycleAdminClient
	AdminAudit       adminv1.AdminAuditClient
	AdminReliability adminv1.AdminReliabilityClient
	AdminStorage     adminv1.StorageAdminClient
	AdminService     adminv1.ServiceAdminClient
	AdminNode        adminv1.NodeAdminClient
	AccessAdmin      adminv1.AccessAdminClient
	Identity         identityv1.IdentityControlClient
	Environment      environmentv1.EnvironmentControlClient
	Function         functionv1.FunctionControlClient
	Run              runv1.RunControlClient
	Secret           secretv1.SecretControlClient
	Service          servicev1.ServiceControlClient
	Catalog          catalogv1.RuntimeCatalogClient
	Tunnel           tunnelv1.TunnelControlClient
	Namespace        namespacev1.NamespaceControlClient
	Quota            quotav1.QuotaControlClient
	Node             nodesandboxv1.NodeSandboxClient
}
