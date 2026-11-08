module github.com/cofy-x/axern/apps/cli

go 1.26.10

require (
	github.com/cofy-x/axern/sdk/go v0.0.0
	github.com/spf13/cobra v1.10.2
	golang.org/x/crypto v0.58.1-0.20261009181718-2f1f834330b2
	golang.org/x/term v0.47.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

require (
	github.com/cofy-x/axern/internal/proto v0.0.0
	github.com/cofy-x/axern/lib/go/grpcclient v0.0.0
	github.com/cofy-x/axern/lib/go/nodecapability v0.0.0
	github.com/go-jose/go-jose/v4 v4.1.9 // indirect; security floor for the complete gRPC module graph
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
)

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

replace github.com/cofy-x/axern/lib/go/grpcclient => ../../lib/go/grpcclient

replace github.com/cofy-x/axern/lib/go/nodecapability => ../../lib/go/nodecapability

replace github.com/cofy-x/axern/internal/proto => ../../internal/proto
