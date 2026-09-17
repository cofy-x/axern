module github.com/cofy-x/axern/internal/proto

go 1.26.8

require (
	github.com/cofy-x/axern/sdk/go v0.0.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/cofy-x/axern/sdk/go => ../../sdk/go
