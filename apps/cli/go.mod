module github.com/cofy-x/axern/apps/cli

go 1.25.12

require (
	github.com/cofy-x/axern/lib/go/agentbundle v0.0.0
	github.com/cofy-x/axern/sdk/go v0.0.0
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

require (
	github.com/cofy-x/axern/lib/go/agentprofile v0.0.0
	github.com/cofy-x/axern/lib/go/grpcclient v0.0.0
	github.com/cofy-x/axern/lib/go/llmproxy v0.0.0
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260427160629-7cedc36a6bc4 // indirect
)

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

replace github.com/cofy-x/axern/lib/go/grpcclient => ../../lib/go/grpcclient

replace github.com/cofy-x/axern/lib/go/agentbundle => ../../lib/go/agentbundle

replace github.com/cofy-x/axern/lib/go/agentprofile => ../../lib/go/agentprofile

replace github.com/cofy-x/axern/lib/go/llmproxy => ../../lib/go/llmproxy
