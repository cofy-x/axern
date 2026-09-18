module github.com/cofy-x/axern/lib/go/memorybudget

go 1.26.8

require (
	github.com/cofy-x/axern/sdk/go v0.0.0
	google.golang.org/protobuf v1.36.12
)

require github.com/go-logr/logr v1.4.4 // indirect

require (
	github.com/cofy-x/axern/internal/proto v0.0.0
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/grpc v1.83.2 // indirect
)

replace github.com/cofy-x/axern/sdk/go => ../../../sdk/go

replace github.com/cofy-x/axern/internal/proto => ../../../internal/proto
