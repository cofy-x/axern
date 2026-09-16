module github.com/cofy-x/axern/apps/cli

go 1.26.8

require (
	github.com/cofy-x/axern/sdk/go v0.0.0
	github.com/spf13/cobra v1.10.2
	golang.org/x/crypto v0.57.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

require (
	github.com/cofy-x/axern/lib/go/grpcclient v0.0.0
	github.com/cofy-x/axern/lib/go/nodecapability v0.0.0
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

replace github.com/cofy-x/axern/lib/go/grpcclient => ../../lib/go/grpcclient

replace github.com/cofy-x/axern/lib/go/nodecapability => ../../lib/go/nodecapability
