package command

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/lib/go/clientconfig"
	axernsdk "github.com/cofy-x/axern/sdk/go"
	"google.golang.org/grpc"
)

func (o *Options) ControlClient(ctx context.Context) (*axernsdk.Client, error) {
	value, err := o.ResolveContext()
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("an Axern context is required")
	}
	options := []axernsdk.ClientOption{axernsdk.WithTLS(value.TLS.CACert, value.TLS.Cert, value.TLS.Key, value.TLS.ServerName), axernsdk.WithProxyMode(value.ProxyMode)}
	if value.ProxyMode == clientconfig.ProxyModeDirect {
		options = append(options, axernsdk.WithDialOptions(grpc.WithNoProxy()))
	}
	return axernsdk.NewClient(ctx, value.Endpoint, options...)
}
