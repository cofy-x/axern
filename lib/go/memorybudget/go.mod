module github.com/cofy-x/axern/lib/go/memorybudget

go 1.26.10

require (
	github.com/cofy-x/axern/sdk/go v0.0.0
	google.golang.org/protobuf v1.36.12
)

require github.com/go-logr/logr v1.4.4 // indirect

require (
	github.com/cofy-x/axern/internal/proto v0.0.0
	github.com/go-jose/go-jose/v4 v4.1.9 // indirect; security floor for the complete gRPC module graph
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/grpc v1.83.2 // indirect
)

replace github.com/cofy-x/axern/sdk/go => ../../../sdk/go

replace github.com/cofy-x/axern/internal/proto => ../../../internal/proto
