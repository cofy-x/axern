module github.com/cofy-x/axern/lib/go/grpcclient

go 1.26.10

require google.golang.org/grpc v1.83.2

require (
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.46.0 // indirect
)

require (
	github.com/go-jose/go-jose/v4 v4.1.9 // indirect; security floor for the complete gRPC module graph
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
