module github.com/cofy-x/axern/runtime/egressd

go 1.26.11

replace github.com/cofy-x/axern/lib/go/networkpolicy => ../../lib/go/networkpolicy

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go

require (
	github.com/cofy-x/axern/lib/go/networkpolicy v0.0.0
	github.com/cofy-x/axern/sdk/go v0.0.0
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/metric v1.47.0
	golang.org/x/net v0.62.0
	golang.org/x/sys v0.49.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require go.opentelemetry.io/otel/log v1.47.0 // indirect

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cofy-x/axern/internal/proto v0.0.0
	github.com/go-jose/go-jose/v4 v4.1.9 // indirect; security floor for the complete gRPC module graph
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
)

replace github.com/cofy-x/axern/internal/proto => ../../internal/proto
