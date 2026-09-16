module github.com/cofy-x/axern/runtime/egressd

go 1.26.8

replace github.com/cofy-x/axern/lib/go/networkpolicy => ../../lib/go/networkpolicy

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

require (
	github.com/cofy-x/axern/lib/go/networkpolicy v0.0.0
	github.com/cofy-x/axern/sdk/go v0.0.0
	go.opentelemetry.io/otel v1.44.0
	go.opentelemetry.io/otel/metric v1.44.0
	golang.org/x/net v0.59.0
	golang.org/x/sys v0.48.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)
